package orderhttp

// order_create_page.go — 后台代客建单页（GET /admin/orders/new + POST /admin/orders/create）。
//
// 立项与四条决策见 docs/02-W-admin-order-create.md：
//
//	1. 入口形态 —— 独立整页（字段 20+ 且明细可多行，抽屉放不下；抽屉关闭态下
//	   「失败即整屏丢输入」更致命）。
//	2. 明细 —— **服务端渲染**的变体候选行（商品目录 ListBundleSKUs + 关键词重渲染），
//	   可用量走只读端口 VariantAvailabilityLookupPort。**不用 /_fragments/**：那是公开面。
//	3. 开号 —— **默认不开号**：开关落在显式请求字段 ProvisionGuestAccount。
//	4. 权限 —— 复用 order:create（见 order_page_router.go 的 CasbinMiddlewareForPath）。
//
// 写失败**就地重渲 200 + 回填 + 出错字段标红**，不走 303：303 之后是一次 GET，请求里没有
// PostForm，几十个字段必然全丢。代价是刷新会重放提交 —— 用 requestId 幂等键兜住。
//
// 回填与错误提示的写法（本页是长表单的样板）：
//   - 回填一律 `formValue(.Echo, "字段名", .Form.X)`；复选框用 `formChecked(...)`；
//   - 字段级错误只有一个**稀疏** .FieldErrors map，模板用 chain 索引读（缺键 falsy），
//     有文案即标红 —— 不需要「handler 全量预填 + 字段清单 + 双向断言」那三层。
//
// 明细分行按**值**对齐而不是按索引 echo：整份明细行由服务端从 PostFormArray 重建后渲染。

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/order/enums"
	"go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

const (
	// orderCreatePageTitle 页面标题（订单模块 enums 里没有这个标题键，
	// 直接用字面量走 shell.Prepare 的 fallback 链路，与订单列表页同一做法）。
	orderCreatePageTitle = "代客建单"
	// orderCreateDefaultRows 明细行的初始行数（可直接填写，不需要先加行）。
	orderCreateDefaultRows = 2
	// orderCreateMaxRows 明细行的渲染上限：行数由 rows 查询参数驱动（无 JS 的加行方式），
	// 不设上限等于让查询串决定页面体积。
	orderCreateMaxRows = 20
	// orderCreateParseLimit 解析并行数组时的防御上限。
	//
	// **不是业务上限** —— 业务上限在 service（maxOrderItems，超限返回 ErrItemLimitExceeded），
	// 在这里再写一份必然与它漂移。这个数只用来挡住「一次提交几十万个字段名」这种请求，
	// 因此刻意取得比业务上限宽松得多。
	orderCreateParseLimit = 500
)

// orderCreateItemRow 明细行：模板按提交顺序渲染，提交时 variantId / quantity 是并行数组。

// orderCreateItemRow 明细行：模板按提交顺序渲染，提交时 variantId / quantity 是并行数组。
type orderCreateItemRow struct {
	VariantID string
	Quantity  string
}

// orderCreateForm 页面上可回填的字段（GET 初始值 / POST 失败回填共用一份形状）。
//
// 回填的是**用户刚打的字**，不回查数据库：新建路径上「库里的旧值」根本不存在。

// orderCreateForm 页面上可回填的字段（GET 初始值 / POST 失败回填共用一份形状）。
//
// 回填的是**用户刚打的字**，不回查数据库：新建路径上「库里的旧值」根本不存在。
type orderCreateForm struct {
	ProjectID        string
	CandidateKeyword string
	Email            string
	CustomerName     string
	CustomerPhone    string
	PayMethod        string
	PayTitle         string
	ShippingTotal    string
	CouponCode       string
	Remark           string
	AdminNote        string
	RequestID        string
	// ProvisionAccount 复选框：默认 false（**不开号**），勾上才开号并发初始密码。
	ProvisionAccount bool
	// SameBilling 复选框：勾上则账单地址不渲染，由服务端复制收货地址。
	SameBilling bool
	// ItemsTooMany 本次提交的明细行数超过解析上限（orderCreateParseLimit）。
	// 超限时**不截断**而是拒绝：截掉的行会凭空消失，而页面上一切正常、订单也建成了 ——
	// 那是最难查的一类数据缺陷。原因由字段级错误给出。
	ItemsTooMany bool
	Ship         orderdto.OrderAddress
	Bill         orderdto.OrderAddress
	Items        []orderCreateItemRow
}

// orderCreatePageHandle 后台代客建单页处理器。

// orderCreatePageHandle 后台代客建单页处理器。
type orderCreatePageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
	// catalog 商品目录（只读用）：候选变体来自 ListBundleSKUs。
	// 传 nil 时页面照常渲染，只是候选为空、明细无法选择（纯单测路径）。
	catalog productcontract.ProductService
	// availability 可用量只读端口：**可选**能力（未装配库存时为 nil）。
	availability productcontract.VariantAvailabilityLookupPort
	// dict 系统字典只读口：只用来给收货 / 账单的国家下拉供数。
	// 可选依赖 —— 未注入时下拉只剩「（不填写）」，页面照常渲染、订单照常可建
	//（国家是选填项，读不到字典不该让建单页打不开）。
	dict sysconfigcontract.DictReader
}

// NewOrderCreatePageHandle 构造。
//
// 可用量走类型断言而不是新增参数：变体可用量是「有则显示、无则降级」的展示信息，
// 缺它不该让建单页打不开（订单列表页的工程列表失败也是同一档处理）。
// dict 是新增参数（与可用量不同）：它来自装配层的同一实例，没有「顺着 catalog 断言出来」
// 的路径 —— 硬凑一条反而会绕开显式的依赖声明。

// NewOrderCreatePageHandle 构造。
//
// 可用量走类型断言而不是新增参数：变体可用量是「有则显示、无则降级」的展示信息，
// 缺它不该让建单页打不开（订单列表页的工程列表失败也是同一档处理）。
// dict 是新增参数（与可用量不同）：它来自装配层的同一实例，没有「顺着 catalog 断言出来」
// 的路径 —— 硬凑一条反而会绕开显式的依赖声明。
func NewOrderCreatePageHandle(orders ordercontract.OrderService,
	projects projectcontract.ProjectService,
	catalog productcontract.ProductService,
	dict sysconfigcontract.DictReader) *orderCreatePageHandle {
	h := &orderCreatePageHandle{orders: orders, projects: projects, catalog: catalog, dict: dict}
	if catalog != nil {
		if lookup, ok := catalog.(productcontract.VariantAvailabilityLookupPort); ok {
			h.availability = lookup
		}
	}
	return h
}

// OrderCreatePage 后台代客建单页（GET /admin/orders/new）。
//
// 查询参数：project（工程）、keyword（候选 SKU 关键词）、rows（明细行数）。
// 三个都是**筛选 / 排版**参数，不承载业务语义，非法值一律归一（不报错）。

// OrderCreatePage 后台代客建单页（GET /admin/orders/new）。
//
// 查询参数：project（工程）、keyword（候选 SKU 关键词）、rows（明细行数）。
// 三个都是**筛选 / 排版**参数，不承载业务语义，非法值一律归一（不报错）。
func (h *orderCreatePageHandle) OrderCreatePage(c *gin.Context) {
	form := orderCreateForm{
		ProjectID:        strings.TrimSpace(c.Query("project")),
		CandidateKeyword: strings.TrimSpace(c.Query("keyword")),
		// 幂等键：本次表单一次性生成，失败重渲时原样带回 —— 用户重试不会落两单。
		RequestID: utils.NewTimeOrderedID(),
	}
	form.Items = make([]orderCreateItemRow, orderCreateRowCount(c.Query("rows")))
	h.render(c, form, nil, "")
}

// OrderCreateSubmit 提交建单（POST /admin/orders/create）。
//
// 三条出口：
//   - 形状不合法（前端能判的那些）→ **200 + 就地重渲**（出错字段标红 + 回填）；
//   - service 拒绝（变体不存在 / 缺货 / 券不可用 …）→ 同样 200 + 就地重渲，
//     原因进顶部提示栏，用户填的东西一个字都不丢；
//   - 成功 → 提示页，1 秒后回到列表并展开新单（orderId 在回跳地址上，不在文案通道里）。
func (h *orderCreatePageHandle) OrderCreateSubmit(c *gin.Context) {
	form := orderCreateFormFromPost(c)
	req, fieldErrs := orderCreateReqFromForm(c, form)
	if len(fieldErrs) > 0 {
		h.render(c, form, fieldErrs, "")
		return
	}

	// 来源与操作人由服务端写入，绝不受表单影响（表单里也没有这些字段）。
	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
	req.CreateBy = shell.CurrentUserID(c)

	res, err := h.orders.CreateAdminOrder(c.Request.Context(), req)
	if err != nil {
		// 业务错误（如「商品规格不存在」「库存不足」）走模块白名单 + 归口文案，
		// 基础设施错误的原文只进日志（orderFacingError 负责这条分档）。
		h.render(c, form, nil, orderFacingError(c, err))
		return
	}

	params := map[string]string{"project": req.ProjectID}
	if res != nil && res.ID != 0 {
		// 带上 orderId：回列表页时那一单的详情是展开的，运营一眼看到刚建的单。
		params["orderId"] = strconv.FormatUint(res.ID, 10)
	}
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      shell.TranslateFor(c)(orderenums.MsgCreateSuccess, "订单已创建"),
		Back:     shell.WithParams("/admin/orders", params),
		BackText: orderBackText(c),
		Seconds:  1,
	})
}

// render 渲染整页（初始态与失败回填态共用同一个出口）。
//
// fieldErrs 为空 map / nil 时页面没有标红字段；pageErr 非空时顶部给错误提示栏。

// render 渲染整页（初始态与失败回填态共用同一个出口）。
//
// fieldErrs 为空 map / nil 时页面没有标红字段；pageErr 非空时顶部给错误提示栏。
func (h *orderCreatePageHandle) render(c *gin.Context, form orderCreateForm, fieldErrs map[string]string, pageErr string) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		// 工程读不出来**不拿走整页**：空列表 + 归口提示 + 200 页壳。
		projects = nil
		pageErr = firstNonEmpty(pageErr, orderFacingError(c, loadErr))
	}

	selected := strings.TrimSpace(form.ProjectID)
	if selected == "" && !loadFailed && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 「没有工程」与「装载失败」是两件事，模板据此渲染两段不同的引导；
	// FormReady 决定要不要渲染表单本体（明细表在它里面 —— 由 handler 算好这个判据，
	// 模板就不必写 if/else 链，静态门禁也不会把空态误判成「吃掉了表头」）。
	noProject := !loadFailed && len(projects) == 0
	formReady := !loadFailed && !noProject

	// 候选变体：关键词只匹配**商品名**（商品目录的既有语义），无分页。
	// 取数失败只影响候选区（页面其余部分照常可用），因此不升级成整页错误。
	var candidates []gin.H
	candidateErr := ""
	if h.catalog != nil && selected != "" {
		skus, cerr := h.catalog.ListBundleSKUs(ctx, &productdto.ListBundleSKUReq{
			ProjectID: selected,
			Keyword:   form.CandidateKeyword,
		})
		if cerr != nil {
			candidateErr = orderFacingError(c, cerr)
		} else {
			candidates = h.candidateViews(c, selected, skus)
		}
	}

	// 字段级错误：**稀疏 map**（只放真的出错的那几个字段）。
	//
	// 旧写法要求 handler 全量预填每一个模板会读的键（Jet 读 map 里缺失的键会让整页中断），
	// 于是维护了一份 orderCreateFieldNames 清单 + 双向断言。现在模板用 **chain 索引**
	// `{{if .FieldErrors["customerEmail"]}}` —— 缺键求值成 nil（falsy），不中断；
	// 于是「有没有错」与「错误文案」合成一件事：有文案即标红，清单随之消失。
	fieldText := make(map[string]string, len(fieldErrs))
	for name, msg := range fieldErrs {
		fieldText[name] = msg
	}

	if len(form.Items) == 0 {
		// 明细表至少要有一行可填的输入框；失败回填时行数按提交的行数来。
		form.Items = []orderCreateItemRow{{}}
	}

	// 「添加明细行」的落点：本页无自定义 JS，加行靠一次 GET 重渲（rows 参数驱动行数）。
	// URL 由 handler 拼（关键词可能含 & 等字符，模板里手拼会破坏查询串）。
	rowAdd := url.Values{}
	rowAdd.Set("project", selected)
	if form.CandidateKeyword != "" {
		rowAdd.Set("keyword", form.CandidateKeyword)
	}
	rowAdd.Set("rows", strconv.Itoa(len(form.Items)+1))

	// 国家/地区下拉（收货与账单各一份）：字典**整表只取一次**，两份视图各自标选中项 ——
	// 一份地址查一次库，等于同一份两百多行的清单在一屏里读两遍。
	tr := shell.TranslateFor(c)
	countries := h.countryList(c)
	shipCountries := countryOptionViews(tr, countries, form.Ship.Country)
	billCountries := countryOptionViews(tr, countries, form.Bill.Country)

	data := shell.Prepare(c, gin.H{
		"title": orderCreatePageTitle,
		"menu":  "orders",
		// 工程上下文（页头下拉；只有一个工程时整段不渲染 —— 单选项的选择器
		// 不提供任何信息，见 admin-ui-logic §2）。
		"Projects":        projects,
		"SelectedProject": selected,
		"LoadFailed":      loadFailed,
		"NoProject":       noProject,
		"FormReady":       formReady,
		// 候选明细（同一份候选给每一行复用：模板按行渲染 N 份 option 列表）。
		"CandidateKeyword": form.CandidateKeyword,
		"Candidates":       candidates,
		"CandidateCount":   len(candidates),
		"CandidateError":   candidateErr,
		"ItemRows":         form.Items,
		"MaxRows":          orderCreateMaxRows,
		// 国家/地区下拉的选项（含首位「（不填写）」空项，国家是选填）。
		"ShipCountries": shipCountries,
		"BillCountries": billCountries,
		"RowAddURL":     "/admin/orders/new?" + rowAdd.Encode(),
		// 错误出口：字段级（标红 + 行内文案）与页级（顶部提示栏）分开。
		"Err": pageErr,
		// FieldErrors 是**稀疏** map：模板用 chain 索引读（缺键 falsy），
		// 有文案即标红，不需要单独的 Invalid 布尔表。
		"FieldErrors":   fieldText,
		"HasFieldError": len(fieldErrs) > 0,
		// 表单初始值与本次提交的原始快照：模板一律写
		// `formValue(.Echo, "字段名", .Form.X)` —— 提交里有这个字段就用提交值
		// （哪怕空串），没有才用初始值。旧写法把 26 个 FormEcho* 键逐个铺开，
		// 还要维护字段清单与模板的双向断言；现在字段名只出现在模板里。
		"Form": form,
		"Echo": couponEcho(c),
	})
	c.HTML(http.StatusOK, "admin/order/order_new.html", data)
}

// countryNoneLabelKey 「（不填写）」空项的 i18n key（中文兜底写在 countryOptionViews 里）。

// countryNoneLabelKey 「（不填写）」空项的 i18n key（中文兜底写在 countryOptionViews 里）。
const countryNoneLabelKey = "admin.order_new.field.country_none"

// countryOptionView 国家/地区下拉的一项（模板只读这三个字段）。

// countryOptionView 国家/地区下拉的一项（模板只读这三个字段）。
type countryOptionView struct {
	Code     string
	Label    string
	Selected bool
}

// countryList 取启用国家清单（字典未接入或读取失败时返回 nil）。
//
// 读失败**不升级成整页错误**：国家是选填项，下拉空着页面照常可用、订单照常可建 ——
// 与「工程列表装载失败 → 空列表 + 归口提示 + 200」同一档处理（真因由 sysconfig 侧记日志）。

// countryList 取启用国家清单（字典未接入或读取失败时返回 nil）。
//
// 读失败**不升级成整页错误**：国家是选填项，下拉空着页面照常可用、订单照常可建 ——
// 与「工程列表装载失败 → 空列表 + 归口提示 + 200」同一档处理（真因由 sysconfig 侧记日志）。
func (h *orderCreatePageHandle) countryList(c *gin.Context) []sysconfigcontract.CountryOption {
	if h.dict == nil {
		return nil
	}
	opts, err := h.dict.ListCountryOptions(c.Request.Context(), response.RequestLanguage(c))
	if err != nil {
		return nil
	}
	return opts
}

// countryOptionViews 国家清单 → 下拉视图。
//
// 首位是「（不填写）」空项：国家**选填** —— 非中国站点、或这单不需要寄的场景下，
// 强行预选一个默认国家会让运营要么照错填、要么每次手动改回来。
//
// 选中项按「已提交的值 → 站点默认国家」定：失败重渲时回填的是运营刚提交的那一个，
// 初次打开时预选站点默认国家（国内后台绝大多数是国内单，省掉一次手选；它只是预选，随时可改）。
//
// 已提交但字典里没有的码会**原样补进列表并选中**：运营在页面上看到的就是会被落库的值，
// 而不是「下拉里没有这一项，于是浏览器悄悄改选了第一项」—— 后者是「改了一个字段、
// 另一个字段跟着变了」这类事故里最难查的一种。

// countryOptionViews 国家清单 → 下拉视图。
//
// 首位是「（不填写）」空项：国家**选填** —— 非中国站点、或这单不需要寄的场景下，
// 强行预选一个默认国家会让运营要么照错填、要么每次手动改回来。
//
// 选中项按「已提交的值 → 站点默认国家」定：失败重渲时回填的是运营刚提交的那一个，
// 初次打开时预选站点默认国家（国内后台绝大多数是国内单，省掉一次手选；它只是预选，随时可改）。
//
// 已提交但字典里没有的码会**原样补进列表并选中**：运营在页面上看到的就是会被落库的值，
// 而不是「下拉里没有这一项，于是浏览器悄悄改选了第一项」—— 后者是「改了一个字段、
// 另一个字段跟着变了」这类事故里最难查的一种。
func countryOptionViews(tr translate, countries []sysconfigcontract.CountryOption, submitted string) []countryOptionView {
	selected := orderdto.NormalizeCountryCode(submitted)
	if selected == "" {
		selected = orderdto.NormalizeCountryCode(i18n.GetDefaultCountry())
	}
	out := make([]countryOptionView, 0, len(countries)+2)
	out = append(out, countryOptionView{Label: tr(countryNoneLabelKey, "（不填写）"), Selected: selected == ""})
	hit := false
	for _, o := range countries {
		code := strings.TrimSpace(o.Code)
		if code == "" {
			continue
		}
		label := strings.TrimSpace(o.Label)
		if label == "" {
			label = code
		}
		matched := selected != "" && strings.EqualFold(code, selected)
		hit = hit || matched
		out = append(out, countryOptionView{Code: code, Label: label, Selected: matched})
	}
	if selected != "" && !hit {
		out = append(out, countryOptionView{Code: selected, Label: selected, Selected: true})
	}
	return out
}

// candidateViews 候选变体 → 模板下拉项（标签在服务端拼好，模板不做算术与取词）。
//
// 标签形态：商品名 · SKU码 · 当前售价（可用 N）。价格是**商品当前售价**，
// 只是给运营选品用的线索；真正入单的单价由服务端按下单快照落库，页面无法左右它。

// candidateViews 候选变体 → 模板下拉项（标签在服务端拼好，模板不做算术与取词）。
//
// 标签形态：商品名 · SKU码 · 当前售价（可用 N）。价格是**商品当前售价**，
// 只是给运营选品用的线索；真正入单的单价由服务端按下单快照落库，页面无法左右它。
func (h *orderCreatePageHandle) candidateViews(c *gin.Context, projectID string,
	skus []*productdto.BundleSKUResp) []gin.H {
	tr := shell.TranslateFor(c)
	avail := map[string]int{}
	if h.availability != nil && len(skus) > 0 {
		ids := make([]string, 0, len(skus))
		for _, sku := range skus {
			if sku != nil && sku.VariantID != "" {
				ids = append(ids, sku.VariantID)
			}
		}
		if len(ids) > 0 {
			// 可用量读不出来只降级（不显示可用量），不影响候选本身 ——
			// 缺货仍会在提交时由库存守卫整体拒绝，那不是静默错误。
			if quantities, err := h.availability.VariantAvailabilities(c.Request.Context(), projectID, ids); err == nil {
				avail = quantities
			}
		}
	}

	out := make([]gin.H, 0, len(skus))
	for _, sku := range skus {
		if sku == nil || sku.VariantID == "" {
			continue
		}
		// 货币符号与订单列表页一致（orderMoneyLabel 的人民币档用的是半角 ¥）；
		// 这里的 Price 是**元**（商品目录的口径），只是给运营的选品线索 ——
		// 真正入单的单价由服务端按下单快照落库（整数分），页面无法左右它，
		// 所以这里不做「元 → 分 → 元」的往返换算（那只会引入浮点误差）。
		label := fmt.Sprintf("%s · %s · ¥%.2f", sku.ProductName, sku.SKUCode, sku.Price)
		if n, ok := avail[sku.VariantID]; ok {
			label += "（" + fmt.Sprintf(tr(orderenums.OrderNewOptionAvailable, "可用 %s"), strconv.Itoa(n)) + "）"
		}
		out = append(out, gin.H{"ID": sku.VariantID, "Label": label})
	}
	return out
}

// orderCreateRowCount 解析明细行数（rows 查询参数）：非法 / 越界一律归一到 [1, orderCreateMaxRows]。

// orderCreateRowCount 解析明细行数（rows 查询参数）：非法 / 越界一律归一到 [1, orderCreateMaxRows]。
func orderCreateRowCount(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return orderCreateDefaultRows
	}
	if n > orderCreateMaxRows {
		return orderCreateMaxRows
	}
	return n
}

// orderCreateFormFromPost 把这次提交的表单摊成回填视图（值只来自这次提交）。

// orderCreateFormFromPost 把这次提交的表单摊成回填视图（值只来自这次提交）。
func orderCreateFormFromPost(c *gin.Context) orderCreateForm {
	form := orderCreateForm{
		ProjectID:        strings.TrimSpace(c.PostForm("project")),
		CandidateKeyword: strings.TrimSpace(c.PostForm("candidateKeyword")),
		Email:            strings.TrimSpace(c.PostForm("customerEmail")),
		CustomerName:     strings.TrimSpace(c.PostForm("customerName")),
		CustomerPhone:    strings.TrimSpace(c.PostForm("customerPhone")),
		PayMethod:        strings.TrimSpace(c.PostForm("paymentMethod")),
		PayTitle:         strings.TrimSpace(c.PostForm("paymentMethodTitle")),
		ShippingTotal:    strings.TrimSpace(c.PostForm("shippingTotal")),
		CouponCode:       strings.TrimSpace(c.PostForm("couponCode")),
		Remark:           strings.TrimSpace(c.PostForm("remark")),
		AdminNote:        strings.TrimSpace(c.PostForm("adminNote")),
		RequestID:        strings.TrimSpace(c.PostForm("requestId")),
		// 复选框：**只有勾选才会提交**（未勾选 = 明确不开号 / 不用同收货地址）。
		ProvisionAccount: c.PostForm("provisionGuestAccount") != "",
		SameBilling:      c.PostForm("sameBilling") != "",
		Ship: orderdto.OrderAddress{
			Name:     strings.TrimSpace(c.PostForm("shipName")),
			Phone:    strings.TrimSpace(c.PostForm("shipPhone")),
			Country:  orderdto.NormalizeCountryCode(c.PostForm("shipCountry")),
			Province: strings.TrimSpace(c.PostForm("shipProvince")),
			City:     strings.TrimSpace(c.PostForm("shipCity")),
			District: strings.TrimSpace(c.PostForm("shipDistrict")),
			Address:  strings.TrimSpace(c.PostForm("shipAddress")),
			Zip:      strings.TrimSpace(c.PostForm("shipZip")),
		},
		Bill: orderdto.OrderAddress{
			Name:     strings.TrimSpace(c.PostForm("billName")),
			Phone:    strings.TrimSpace(c.PostForm("billPhone")),
			Country:  orderdto.NormalizeCountryCode(c.PostForm("billCountry")),
			Province: strings.TrimSpace(c.PostForm("billProvince")),
			City:     strings.TrimSpace(c.PostForm("billCity")),
			District: strings.TrimSpace(c.PostForm("billDistrict")),
			Address:  strings.TrimSpace(c.PostForm("billAddress")),
			Zip:      strings.TrimSpace(c.PostForm("billZip")),
		},
	}
	form.Items, form.ItemsTooMany = orderCreateItemRowsFromPost(c)
	if form.RequestID == "" {
		form.RequestID = utils.NewTimeOrderedID()
	}
	return form
}

// orderCreateItemRowsFromPost 从并行数组重建整份明细行（**按提交顺序**，不做索引 echo）。
//
// variantId / quantity 由浏览器按 DOM 顺序各提交一份数组，这里逐下标配对；
// 行结构只由模板决定（每一行都提交这两个字段，没有任何行被 disabled —— 一旦某行
// 不提交，后面的行就会整体错位一格，表现为「数量跑到别的商品上」且不报错）。
//
// 第二个返回值 = 「提交的行数超过解析上限」。它存在的意义是**把上限做成一次明确的拒绝
// 而不是截断**：静默截断会让超出的明细凭空消失（订单照样建成、页面一切正常），
// 这类缺陷事后只能靠对账发现，而项目规则明令禁止静默丢弃一行。

// orderCreateItemRowsFromPost 从并行数组重建整份明细行（**按提交顺序**，不做索引 echo）。
//
// variantId / quantity 由浏览器按 DOM 顺序各提交一份数组，这里逐下标配对；
// 行结构只由模板决定（每一行都提交这两个字段，没有任何行被 disabled —— 一旦某行
// 不提交，后面的行就会整体错位一格，表现为「数量跑到别的商品上」且不报错）。
//
// 第二个返回值 = 「提交的行数超过解析上限」。它存在的意义是**把上限做成一次明确的拒绝
// 而不是截断**：静默截断会让超出的明细凭空消失（订单照样建成、页面一切正常），
// 这类缺陷事后只能靠对账发现，而项目规则明令禁止静默丢弃一行。
func orderCreateItemRowsFromPost(c *gin.Context) (rows []orderCreateItemRow, tooMany bool) {
	ids := c.PostFormArray("variantId")
	quantities := c.PostFormArray("quantity")
	if len(ids) > orderCreateParseLimit {
		return []orderCreateItemRow{{}}, true
	}
	rows = make([]orderCreateItemRow, 0, len(ids))
	for i, raw := range ids {
		qty := ""
		if i < len(quantities) {
			qty = strings.TrimSpace(quantities[i])
		}
		rows = append(rows, orderCreateItemRow{VariantID: strings.TrimSpace(raw), Quantity: qty})
	}
	if len(rows) == 0 {
		rows = append(rows, orderCreateItemRow{})
	}
	return rows, false
}

// orderCreateReqFromForm 表单 → CreateOrderReq，并给出**字段级**的错误（标红用）。
//
// 只做前端也判得出的形状校验（必填 / 邮箱形状 / 至少一行明细），判据与 service 的
// validateCreateOrderReq 一致 —— 但真值仍在 service：这里放过的请求它会再校验一遍
// （规格归属、启用状态、数量区间、库存、券），不变量以 service 为准。
//
// 返回值里的 fieldErrs 非空时**不调用** service：明知缺必填还打一次库没有意义。

// orderCreateReqFromForm 表单 → CreateOrderReq，并给出**字段级**的错误（标红用）。
//
// 只做前端也判得出的形状校验（必填 / 邮箱形状 / 至少一行明细），判据与 service 的
// validateCreateOrderReq 一致 —— 但真值仍在 service：这里放过的请求它会再校验一遍
// （规格归属、启用状态、数量区间、库存、券），不变量以 service 为准。
//
// 返回值里的 fieldErrs 非空时**不调用** service：明知缺必填还打一次库没有意义。
func orderCreateReqFromForm(c *gin.Context, form orderCreateForm) (*orderdto.CreateOrderReq, map[string]string) {
	fieldErrs := map[string]string{}
	tr := shell.TranslateFor(c)

	if form.Email == "" {
		fieldErrs["customerEmail"] = tr(orderenums.ErrCustomerEmailRequired, orderenums.ErrCustomerEmailRequired)
	} else if !strings.Contains(form.Email, "@") || len(form.Email) < 3 {
		fieldErrs["customerEmail"] = tr(orderenums.ErrCustomerEmailInvalid, orderenums.ErrCustomerEmailInvalid)
	}

	items := make([]orderdto.OrderItemReq, 0, len(form.Items))
	for _, row := range form.Items {
		if row.VariantID == "" {
			// 空行是合法的：运营加多了行没填，跳过即可（不是错误）。
			continue
		}
		qty, _ := strconv.Atoi(row.Quantity)
		items = append(items, orderdto.OrderItemReq{VariantID: row.VariantID, Quantity: qty})
	}
	if form.ItemsTooMany {
		// 超过解析上限与「一行都没填」是两件事 —— 只说后者会让人以为忘了填，白找一轮。
		fieldErrs["items"] = tr(orderenums.ErrItemLimitExceeded, orderenums.ErrItemLimitExceeded)
	} else if len(items) == 0 {
		fieldErrs["items"] = tr(orderenums.ErrItemsRequired, orderenums.ErrItemsRequired)
	}

	shippingTotal, _ := strconv.ParseInt(strings.TrimSpace(form.ShippingTotal), 10, 64)

	// 勾了「同收货地址」就由服务端复制：让调用方各填两份地址，迟早出现两份不一致的账单地址。
	billing := form.Bill
	if form.SameBilling {
		billing = form.Ship
	}

	// 开号开关恒为**显式值**：本页是后台入口，不勾选就是明确的 false（不开号），
	// 不存在「未表态」这一档 —— 未表态（nil）只留给既有前台 checkout 链路。
	provision := form.ProvisionAccount

	return &orderdto.CreateOrderReq{
		ProjectID:             form.ProjectID,
		CustomerEmail:         form.Email,
		CustomerName:          form.CustomerName,
		CustomerPhone:         form.CustomerPhone,
		Items:                 items,
		Shipping:              form.Ship,
		Billing:               billing,
		PaymentMethod:         form.PayMethod,
		PaymentMethodTitle:    form.PayTitle,
		ShippingTotal:         shippingTotal,
		CouponCode:            form.CouponCode,
		Remark:                form.Remark,
		AdminNote:             form.AdminNote,
		RequestID:             form.RequestID,
		ProvisionGuestAccount: &provision,
	}, fieldErrs
}

// order_page_handle.go — 后台订单管理页（BIZ-1 销售侧）。

//

// 订单模块的后台 API 已就绪（8 个接口挂在 /api/order/* 的三层链上），但后台没有管理界面。

// 本文件补齐这个入口：一个页面同时给出工程切换、状态计数、组合筛选、订单列表、

// 详情（头 / 订单项 / 状态流转链）与三个写操作，**无 JS 也能用** ——

// 所有链接都是普通 GET，所有写操作都是原生表单 POST + csrf_token 隐藏域。

//

// 三条与订单模块的约定：

//

//  1. 跨模块只依赖 ordercontract.OrderService 与不可变 orderdto，

//     **不 import 订单模块的 model 包**：状态常量与合法流转边在这里各有一份

//     展示用的字面量表（orderStatusViews / orderNextStatuses），真值始终在服务端。

//     本页不判断流转是否合法，选错就由服务端拒绝、把它的中文错误回显到页面顶部。

//

//  2. 金额一律**整数分**（商品域是元，换算发生在商品域边界上）。

//     分 → 元的换算只在 orderAmountText 里发生一次，模板不做算术 ——

//     两处换算迟早会分叉，而订单页分叉出来的差额是要对账的。

//

//  3. 操作人（operatorType / operatorId / operatorName）由 handler 从会话覆盖写入，

//     表单里不存在这三个字段：能被客户端伪造的操作人，等于审计上没有操作人。

const (
	// orderOperatorTypeAdmin 后台操作人类型（表单不传，由 handler 覆盖写入）。
	orderOperatorTypeAdmin = "admin"
	// orderFieldEmpty 空字段的展示占位：表格里的空白单元格读不出「没有值」。
	orderFieldEmpty = "—"
)

// 订单状态取值（展示用字面量，刻意不 import 订单模块的 model 包）。
