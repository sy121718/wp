package producthttp

import (
	"errors"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_bundle.go — 捆绑商品（容器 + 可选组）：配置页、成员解析接口与详情页的捆绑面板。
//
// 可用量来自库存真源端口（warehouseOptions / warehouseSKUOptions）：捆绑的「可卖几份」
// 由各成员的最小可用量决定，不在商品表里存。

// ProductBundlePage 捆绑配置页。
// 与商品页同族（挂在 productPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。

// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 前端只是便捷入口：ProductsCreate 会带着仓库 id 让 service 在**该仓**重新复核一遍。
//
// 接口面（原先的 product_handle.go 捆绑段）在前，后台页面在后。

// productBundlePageTemplate / productBundleFormTemplate 整页与不带页壳两档的模板。
//
// 同一份装配、同一份视图，区别只在套不套页壳（htmx 换的是页面里那一块，把整页塞进去
// 会把外层结构一起换掉）。不是两种响应形态：后端给数据、Jet 渲染 HTML。
const productBundlePageTemplate = "admin/product/product_bundle.html"
const productBundleFormTemplate = "admin/product/product_bundle_form.html"

// bundleResolveURL 成员来源面板的解析接口（URL 形状属 HTTP，控制器给）。
const bundleResolveURL = "/admin/products/bundle/members/resolve"

// ProductBundlePage 捆绑配置页。
// 与商品页同族（挂在 productPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。
//
// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 接口面（原先的 product_handle.go 捆绑段）在前，后台页面在后。
func (h *productPageHandle) ProductBundlePage(c *gin.Context) {
	h.renderBundlePage(c, nil, "", false)
}

// bundleSubmittedFormFromRequest 把本次提交的成员表**原样**读出来（并行数组 + 三个配置数值）。
//
// 与保存路径读的是同一批字段，但这里**不清理也不跳过空白行**：渲染要保住空行占的位置
// （行与数组按 DOM 顺序一一对应，摘掉空行会让它后面的成员整体上移）。
func bundleSubmittedFormFromRequest(c *gin.Context) *productdto.BundleSubmittedForm {
	form := &productdto.BundleSubmittedForm{
		VariantIDs:    c.PostFormArray("variantId"),
		Required:      c.PostFormArray("required"),
		DefaultQty:    c.PostFormArray("defaultQty"),
		MinQty:        c.PostFormArray("minQty"),
		MaxQty:        c.PostFormArray("maxQty"),
		SourceKinds:   c.PostFormArray("sourceKind"),
		WarehouseIDs:  c.PostFormArray("memberWarehouseId"),
		WarehouseSKUs: c.PostFormArray("memberWarehouseSku"),
		ExternalSKUs:  c.PostFormArray("memberExternalSku"),
		MinTotalQty:   atoiOrZero(c.PostForm("minTotalQty")),
		MaxTotalQty:   atoiOrZero(c.PostForm("maxTotalQty")),
	}
	if v, aerr := strconv.Atoi(strings.TrimSpace(c.PostForm("maxOptions"))); aerr == nil && v > 0 {
		form.MaxOptions = v
	}
	return form
}

func (h *productPageHandle) renderBundlePage(c *gin.Context, submitted *productdto.BundleSubmittedForm, failure string, fragment bool) {
	reader, ok := h.products.(productcontract.ProductPageReader)
	if !ok {
		// 装配缺陷：读模型没接上。给 500 而不是渲染成一张能保存的空表单 ——
		// 否则运营会照着一张「配置读不出来」的空表去配，且提交也会失败。
		logger.Scene(productPageScene).Error(errBundleReadModel, "捆绑配置页取数无处可去：读模型未注入")
		missing := bundlePageFailureView(http.StatusInternalServerError)
		adminui.RenderPage(c, productBundlePageTemplate, &missing)
		return
	}
	view, err := reader.ProductBundlePage(c.Request.Context(), &productdto.ProductBundlePageReq{
		ProjectID:         bundleQueryParam(c, "project", "projectId"),
		ProductID:         bundleQueryParam(c, "product", "productId"),
		Source:            c.Query("source"),
		SourceProductID:   c.Query("sourceProduct"),
		SourceWarehouseID: c.Query("sourceWarehouse"),
		// Submitted 是**写路径的产物**：它在请求体里，读模型不认识 gin.Context。
		Submitted: submitted,
	})
	if view == nil {
		// 契约上视图恒非 nil；契约是约定而不是类型保证，这一层兜底必须存在。
		failed := bundlePageFailureView(http.StatusInternalServerError)
		view = &failed
		err = errors.New("捆绑配置页的视图为空")
	}
	if err != nil {
		// 归口文案（中英 / 缺词 fallback 都在里面）；驱动原文与原始错误只进日志。
		view.LoadError = productErrPageText(c, err)
	}

	// 剩下三样都只由控制器给：写失败的成品串（Err）、两个 URL 形状（ResolveURL / ListQuery）。
	// 它们要么来自请求，要么是 HTTP 侧的形状 —— 都不是取数结论。
	extra := gin.H{
		"Err":        failure,
		"ResolveURL": bundleResolveURL,
		// 页面上下文（表单 action 的 query）：保存失败 / 成功时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productBundleBackKeys...),
	}
	// 这一屏不是可缓存的成功页：重试必须真的回到服务端（判据由视图给，见 PageNoStore）。
	if view.PageNoStore() {
		c.Header("Cache-Control", "no-store")
	}
	if fragment {
		// 不带页壳档（htmx 写失败重渲染）：换的是页面里那一块，整页 HTML 塞进这一块
		// 会把外层结构一起换掉 —— 所以这一档**仍是 200**。
		// 失败信息进两个槽：Err 是这次保存没成功的原因（调用方给），LoadError 是「这一页
		// 没能重建出来」；有提交快照时照样渲染回填表单（保存禁用），
		// 否则 hx-swap=outerHTML 会把用户整块表单换成一句提示。
		c.HTML(http.StatusOK, productBundleFormTemplate, bundlePageData(c, view, extra))
		return
	}
	if err != nil && submitted != nil {
		// 原生 POST 写失败 + 重读也失败：整页把提交快照渲染回来。
		//
		// 状态码保持 200：这不是「整页 GET 读不出来」（那一条是 500 + 失败槽），而是
		// 「写没成功、页面重建失败」—— 响应本身是完整可用的页面（表单在、保存禁用、
		// 给了「重新载入」入口），把用户刚输入的内容换成一个失败槽才是更坏的结果。
		c.HTML(http.StatusOK, productBundlePageTemplate, bundlePageData(c, view, extra))
		return
	}
	// 整页档：出口只有这一处（页面成败都走它）。没有提交快照的装载失败 = 失败槽 + 状态码。
	c.HTML(view.PageStatus(), productBundlePageTemplate, bundlePageData(c, view, extra))
}

// errBundleReadModel 读模型未注入时的日志事由（不出口，只进日志）。
var errBundleReadModel = errors.New("商品页面读模型未注入（装配缺陷）")

// bundlePageData 摊平视图 + 叠加控制器那三样键（写失败的成品串与两个 URL 形状）。
//
// 用 ViewMap 而不是 adminui.RenderPage：这一屏有三个出口（两个 200 + 一档跟随视图档位），
// 而视图自己已经实现 PageResult —— 状态码与 no-store 按页面的结论给，
// 视图与控制器侧那三样键在这里合成一份数据。
func bundlePageData(c *gin.Context, view *productdto.ProductBundlePageView, extra gin.H) gin.H {
	data := adminui.ViewMap(view)
	for k, v := range extra {
		data[k] = v
	}
	return adminui.Prepare(c, data)
}

// bundlePageFailureView 失败档的视图（给齐模板必需键，避免失败分支只设了错误键）。
func bundlePageFailureView(status int) productdto.ProductBundlePageView {
	return productdto.ProductBundlePageView{
		Title:            productenums.ProductBundleTitle,
		Menu:             "products",
		Projects:         []projectdto.ProjectResp{},
		Products:         []*productdto.ProductResp{},
		Skus:             []*productdto.BundleSKUResp{},
		Rows:             []productdto.BundleOptionRow{},
		CandidateSKUs:    []productdto.BundleCandidateView{},
		Sources:          []map[string]any{},
		SourceProducts:   []map[string]any{},
		SourceAttributes: []*productdto.AttributeResp{},
		SourceWarehouses: []productdto.BundleSourceWarehouseGroupView{},
		MaxOptions:       productdto.BundleMaxOptionsLimit,
		SaveBlocked:      true,
		LoadFailed:       status != http.StatusOK,
		HTTPStatus:       status,
	}
}

// productBundleNoProductText 捆绑页的参数级提示（未选商品就提交保存），走整页提示页。
//
// key + 中文兜底：写侧取词（英文站点不再显示中文）。
const productBundleNoProductFallback = "请先选择商品"

// productBundleNoProductMsg 上面那条提示的**事实**（key + 中文兜底，取词在提示页模板里）。
var productBundleNoProductMsg = productMsg{Key: productenums.ProductBundleNoProduct, Fallback: productBundleNoProductFallback}

// strAt 取并行数组的第 i 个字符串（缺失一律空串）。
//
// 与 atoiAt 同一手法：这里不承担校验 —— 来源字段的白名单与清理在 service
// （未知来源会被明确拒绝，不是在这里静默丢弃）。
func strAt(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[i])
}

// ProductBundleSave 保存捆绑配置；失败在当前请求回填，HX 成功整页跳转。
func (h *productPageHandle) ProductBundleSave(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	if productID == "" {
		productBundleJump(c, false, productBundleNoProductMsg)
		return
	}
	cfg := productdto.NewEmptyBundleConfig()
	if v, aerr := strconv.Atoi(strings.TrimSpace(c.PostForm("maxOptions"))); aerr == nil && v > 0 {
		cfg.MaxOptions = v
	}
	cfg.MinTotalQty = atoiOrZero(c.PostForm("minTotalQty"))
	cfg.MaxTotalQty = atoiOrZero(c.PostForm("maxTotalQty"))
	ids := c.PostFormArray("variantId")
	required := c.PostFormArray("required")
	defaults := c.PostFormArray("defaultQty")
	mins := c.PostFormArray("minQty")
	maxes := c.PostFormArray("maxQty")
	// 来源快照是**并行数组**（与上面五个同一种形态）：成员表的每一行都提交这四个字段，
	// 因此索引与 variantId 一一对应。来源只作溯源与展示，服务端仍按白名单归一
	//（未知来源在 service 里被拒，非仓库来源的仓库字段被清空）。
	sourceKinds := c.PostFormArray("sourceKind")
	warehouseIDs := c.PostFormArray("memberWarehouseId")
	warehouseSKUs := c.PostFormArray("memberWarehouseSku")
	externalSKUs := c.PostFormArray("memberExternalSku")
	for i, vid := range ids {
		vid = strings.TrimSpace(vid)
		if vid == "" {
			continue
		}
		cfg.Options = append(cfg.Options, productdto.BundleOption{
			VariantID:  vid,
			Required:   atoiAt(required, i) == 1,
			DefaultQty: atoiAt(defaults, i),
			MinQty:     atoiAt(mins, i),
			MaxQty:     atoiAt(maxes, i),

			SourceKind:   strAt(sourceKinds, i),
			WarehouseID:  strAt(warehouseIDs, i),
			WarehouseSKU: strAt(warehouseSKUs, i),
			ExternalSKU:  strAt(externalSKUs, i),
		})
	}
	if _, err := h.products.SetBundleConfig(ctx, &productdto.SetBundleConfigReq{
		ProductID: productID,
		Config:    cfg,
		// 操作人只从会话取（主数据变更记录要记「谁改的」）。
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		// 失败**原地重渲染**（保留用户刚配的成员清单），不跳提示页：
		// 成员表是这一屏里最贵的内容，跳走等于让它整屏消失（见 internal/templates/CLAUDE.md
		// 「写表单失败时原地留住输入」）。htmx 档换片段、原生档换整页。
		q := url.Values{"product": {productID}}
		if project := strings.TrimSpace(c.PostForm("projectId")); project != "" {
			q.Set("project", project)
		}
		c.Request.URL.RawQuery = q.Encode()
		h.renderBundlePage(c, bundleSubmittedFormFromRequest(c), productErrText(c, err), isHXRequest(c))
		return
	}
	// 成功走整页提示（htmx 由 shell.RenderJump 输出 HX-Redirect）——取代原先的 302 / HX-Redirect。
	productBundleJump(c, true, productActionDoneMsg)
}

// bundleQueryParam 取本页的上下文参数（工程 / 商品），按候选名依次认第一个非空值。
//
// 参数名有两套且都合法：本页自己的两个选择器用 project / product；商品详情页的
// 「编辑捆绑构成」入口用 projectId / productId（与商品页其余表单同一套字段名，
// 直接复用商品 id 不会串）。两个都认，入口从哪来都能选中同一个商品 ——
// 少认一个的表现是「点入口进来是空骨架」，而页面上看不出哪里不对。
func bundleQueryParam(c *gin.Context, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(c.Query(name)); v != "" {
			return v
		}
	}
	return ""
}

// 与变体清单的预览端点（/admin/products/variant/preview）同一形态：服务端把候选行与逐条
// 跳过原因算好回 JSON，前端只负责把行**追加**进成员清单；「保存配置」才是落库动作。
//
// 为什么要 JSON 而不是服务端渲染的片段：成员清单是**未保存的前端状态**（与变体清单同理），
// 服务端渲染的片段落不进那个状态里；这里回的行结构与服务端渲染的行结构逐字一致。
//
// 失败回 400 + 可读文案（走 productErrText，不把 enums 裸 key 或 PG 报错铺给前端）。
func (h *productPageHandle) ProductsBundleMembersResolve(c *gin.Context) {
	ctx := c.Request.Context()
	req := &productdto.ResolveBundleMembersReq{
		ProductID:       strings.TrimSpace(c.PostForm("productId")),
		ProjectID:       strings.TrimSpace(c.PostForm("projectId")),
		Source:          strings.TrimSpace(c.PostForm("source")),
		SourceProductID: strings.TrimSpace(c.PostForm("sourceProductId")),
		WarehouseID:     strings.TrimSpace(c.PostForm("sourceWarehouse")),
		WarehouseSKUs:   c.PostFormArray("warehouseSku"),
		// 属性组合的勾选用生成组合抽屉的同一套字段名（attr:<属性组 id>=值 id）：
		// 收拢规则只有一份，两个入口不会一个认前缀、另一个漏读。
		Selections: variantSelectionFromForm(c),
		// 前端清单里已有的成员：服务端据此去重（同一变体只出现一次）。
		ExistingVariantIDs: c.PostFormArray("existingVariantId"),
	}
	res, err := h.products.ResolveBundleMembers(ctx, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": productErrText(c, err)})
		return
	}
	// 例外（JSON 出口的成品串）：这个端点的响应体是给前端直接渲染的（sourceLabel / skips.text），
	// 没有模板取词这一层 —— 与页面槽位不同，这里的字符串必须已经是当前语言的成品。
	tr := shell.TranslateFor(c)
	// 规格文本要用来源商品的属性组：「组合在商品侧没有对应变体」必须指得出是哪一组。
	attrs := []*productdto.AttributeResp{}
	if req.SourceProductID != "" {
		if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: req.SourceProductID}); derr == nil && detail != nil {
			attrs = detail.Attributes
		}
	}
	members := make([]gin.H, 0, len(res.Members))
	for _, m := range res.Members {
		if m == nil {
			continue
		}
		// 键名一律小驼峰：gin.H 是 map，JSON 键就是这里的字面量 ——
		// 页面 JS 按 variantId / sourceKind / warehouseSku 取值（与其余 JSON 接口同一口径），
		// 写成 PascalCase 会让前端拿到 undefined 却看不出任何错。
		members = append(members, gin.H{
			"variantId":   m.VariantID,
			"skuCode":     m.SKUCode,
			"productId":   m.ProductID,
			"productName": m.ProductName,
			"spec":        specLabel(m.OptionValues, attrs),
			"sourceKind":  m.Source.Kind,
			"sourceLabel": bundleSourceLabel(tr, productdto.BundleOption{
				SourceKind: m.Source.Kind, WarehouseID: m.Source.WarehouseID,
				WarehouseSKU: m.Source.WarehouseSKU, ExternalSKU: m.Source.ExternalSKU,
			}),
			"warehouseId":  m.Source.WarehouseID,
			"warehouseSku": m.Source.WarehouseSKU,
			"externalSku":  m.Source.ExternalSKU,
		})
	}
	skips := make([]gin.H, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		// 跳过的行要能被认出来：「从规格组合来的」用可读规格文本，「从仓库来的」用仓库 SKU 文本。
		label := strings.TrimSpace(s.SKUCode)
		if label == "" {
			label = strings.TrimSpace(s.WarehouseSKU)
		}
		if label == "" {
			label = strings.TrimSpace(s.VariantID)
		}
		if spec := specLabel(s.OptionValues, attrs); spec != "" && spec != "—" {
			if label == "" {
				label = spec
			} else {
				label = label + " · " + spec
			}
		}
		skips = append(skips, gin.H{
			"reason": s.Reason,
			"text":   tr(s.Reason, bundleMemberSkipFallbacks[s.Reason]),
			"label":  label,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "added": len(members), "skipped": len(skips),
		"members": members, "skips": skips,
	})
}

// bundleMemberSkipFallbacks 成员来源解析被跳过时的中文兜底（key 见 productenums.BundleMember*）。
//
// 与 variantSkipFallbacks 同一形态：词条缺失时页面上仍是可读的一句话，
// 而不是裸 key。
var bundleMemberSkipFallbacks = map[string]string{
	productenums.BundleMemberNotOnProduct:        "该属性值组合在商品侧没有对应变体，未加入（请先到该商品上生成这个规格的变体）",
	productenums.BundleMemberSkippedInList:       "该 SKU 已在成员清单里，未重复加入",
	productenums.BundleMemberWarehouseSKUMissing: "该仓库里没有这条仓库 SKU，未加入（请确认仓库选对了，或先在该仓建好这条货）",
	productenums.BundleMemberVariantDisabled:     "该变体已停用，未加入（停用的 SKU 挂进套餐会变成前台选不了又躲不开的必选项）",
	productenums.BundleMemberOptionsExceeded:     "已达该捆绑配置的选项数量上限，未加入（可先调大上限再解析）",
	productenums.ErrBundleSelfReference:          "该 SKU 属于这个捆绑容器自己，未加入（不能自引用）",
}

// atoiAt 取并行数组的第 i 个并转整数（缺失 / 非法一律 0）。
//
// 不用「解析失败即报错」：这里的 0 会进入 service 的配置校验，
// 该拒的（负数、必选数量为 0 等）由那边统一给出可读原因，前端只负责搬运。
func atoiAt(values []string, i int) int {
	if i < 0 || i >= len(values) {
		return 0
	}
	return atoiOrZero(values[i])
}

// atoiOrZero 解析整数，非法一律 0。
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// isBundleProduct 商品类型是否为捆绑容器（详情页据此决定要不要渲染「捆绑构成」）。
//
// 判据只认类型本身，不看「bundle_items 是否非空」——后者是迁移 238 之前的旧口径，
// 分不清「主体卖自己的 SKU」与「容器卖组合」（bundle 商品的价 0 变体就是这么来的）。
func isBundleProduct(productType string) bool {
	return productType == productmodel.TypeBundle
}

// bundleSourceParts 成员来源快照的**事实段**（仅用于展示 —— 来源不是身份）。
//
// 「从仓库选」再多带两段：仓库 SKU 与外部编码 —— 运营对账时手上可能是其中任意一个；
// 仓库 id 留在 JSON 里做溯源，不搬上表格（uuid 对读的人没有含义）。
//
// 第一段恒存在（来源词条），后两段按数据有无增减 —— 模板据段数为 0 渲染「—」。
// 来源 → (key, 兜底) 的映射在 productenums.BundleSourceLabel（空 kind = 历史配置 / 手工指定）。
func bundleSourceParts(o productdto.BundleOption) []productdto.BundleSourcePart {
	return productservice.BundleSourceParts(o)
}

// bundleSourceLabel 来源事实段 → 当前语言的成品串。
//
// **只有 JSON 出口用它**（ProductsBundleMembersResolve 的 `sourceLabel`：前端把它直接显示，
// 没有模板取词这一层）—— 属于「JSON 出口的成品串」例外，调用点的 tr 同理。
// 模板侧一律用 bundleSourceParts 交事实、在模板里取词与拼接。
func bundleSourceLabel(tr func(key, fallback string) string, o productdto.BundleOption) string {
	segments := make([]string, 0, 3)
	for _, part := range bundleSourceParts(o) {
		text := tr(part.LabelKey, part.LabelFallback)
		if part.Value != "" {
			text += " " + part.Value
		}
		segments = append(segments, text)
	}
	return strings.Join(segments, " · ")
}

// bundlePanelRows 捆绑构成表的行：成员 SKU 的规则、可用量与后台价格口径。
//
// 成员挂牌价（ItemPrice）在这里只作**参考**：套餐金额只由容器价得出，成员价不参与
// 任何计价与前台展示（迁移 238 的类型不变量）；成员成本只进后台口径。
//
// SKU 已被删除时配置里只剩 variantId（SKUCode 为空），照常出一行并标注 ——
// 静默丢项会让人以为配置里本来就没有这一项，也就不会去修它。
func bundlePanelRows(options []*productdto.BundleOptionDetail) []gin.H {
	rows := make([]gin.H, 0, len(options))
	for _, o := range options {
		if o == nil {
			continue
		}
		// Missing 表示配置指向的变体已经不存在（与「存在但被停用」是两回事，
		// 状态文案与可行动作都不同，故不合并成一个 bool）。
		missing := o.SKUCode == ""
		itemPrice, costPrice := formatAmount(o.ItemPrice), formatNullableAmount(o.CostPrice)
		if missing {
			// 变体没了，价格与成本都无从谈起：这里的 0 会被读成「这个成员不要钱」。
			itemPrice, costPrice = "—", "—"
		}
		rows = append(rows, gin.H{
			"VariantID":   o.VariantID,
			"SKUCode":     o.SKUCode,
			"ProductName": o.ProductName,
			"Required":    o.Required,
			"DefaultQty":  o.DefaultQty,
			"MinQty":      o.MinQty,
			"MaxQty":      o.MaxQty,
			"Available":   o.Available,
			"ItemPrice":   itemPrice,
			"CostPrice":   costPrice,
			"Enabled":     o.Enabled,
			"Missing":     missing,
			// 来源快照（仅展示）：事实段，取词与分段拼接在模板；空 = 手工指定 / 历史配置。
			"SourceParts": bundleSourceParts(o.BundleOption),
		})
	}
	return rows
}

// bundlePanel 商品详情页「捆绑构成」区块的数据（仅 type=bundle 调用）。
//
// 读不出配置时**不静默**：区块照常渲染并带一条可读原因（区块本身就是运维发现
// 「配置读不出来」的地方，整块消失会让人以为这个商品不是捆绑品）。
// 错误原因只给词条文案，不铺 err.Error()（内部包装文案对运营不可行动）。
func (h *productPageHandle) bundlePanel(c *gin.Context, projectID, productID string) gin.H {
	panel := gin.H{
		"Loaded":    false,
		"ErrorText": "",
		// 容器价是套餐金额的唯一来源，读不出来时给 —（不拿成员价兜底：那是错的口径）。
		"BasePrice":   "—",
		"Options":     []gin.H{},
		"OptionCount": 0,
	}
	detail, err := h.products.GetBundleConfig(c.Request.Context(), &productdto.GetBundleConfigReq{
		ProductID: productID,
		// 工程显式传下去（DB-009）：配置详情含库存可用量，走真源查询要在工程作用域里。
		// 后台页不走 resolveProjectID 的唯一工程兜底（多工程下它会直接判参数错）。
		ProjectID: projectID,
	})
	if err != nil || detail == nil {
		panel["ErrorText"] = productMsgText(c, bundlePanelReadFailedMsg)
		return panel
	}
	rows := bundlePanelRows(detail.Options)
	panel["Loaded"] = true
	panel["BasePrice"] = formatAmount(detail.BasePrice)
	panel["Options"] = rows
	panel["OptionCount"] = len(rows)
	return panel
}

// productStockCell 商品列表「库存」列的数据（三态 + 分仓明细）。
//
// 三态口径（docs/14 §1.4）：任一仓不跟踪 → ∞（**绝不求和**，求和等于把无限当 0）；
// 全部跟踪 → 各仓数量之和（0 就显示 0）；一个仓都没有这一行 → 未入库。
//
// 分仓明细按**工程仓库清单**逐仓列出：服务端返回的 StockWarehouses 只含有库存行的仓，
// 「某仓没有行」同样是一条信息（该仓未入库）—— 只列有行的仓会让运营以为这个商品只在那几个仓。
//
// 文案（∞ / 未入库）留在模板侧用 tr() 取词，这里只给出状态与数字：
// 一种语言一个值，别把中文拼进数据层（接口调用方拿到的会是中文）。
func productStockCell(detail *productdto.ProductResp, warehouses []productdto.WarehouseOptionView) gin.H {
	cell := gin.H{
		"State": productenums.StockStateNone,
		"Total": 0,
		"Rows":  []gin.H{},
	}
	if detail == nil {
		return cell
	}
	if detail.StockState != "" {
		cell["State"] = detail.StockState
	}
	cell["Total"] = detail.StockTotal
	byWarehouse := make(map[string]*productdto.ProductWarehouseStockResp, len(detail.StockWarehouses))
	for _, row := range detail.StockWarehouses {
		if row != nil {
			byWarehouse[row.WarehouseID] = row
		}
	}
	rows := make([]gin.H, 0, len(warehouses))
	for _, w := range warehouses {
		item := gin.H{"WarehouseID": w.ID, "WarehouseLabel": w.Label,
			"State": productenums.StockStateNone, "Quantity": 0}
		if row, ok := byWarehouse[w.ID]; ok {
			item["State"] = row.State
			item["Quantity"] = row.Quantity
			if row.SKUCode != "" {
				item["SKUCode"] = row.SKUCode
			}
		}
		rows = append(rows, item)
	}
	cell["Rows"] = rows
	return cell
}

// bundlePanelReadFailedFallback 捆绑配置读取失败的中文兜底（key 在
// productenums.ProductBundleDetailFailed）。
//
// 与捆绑配置页同一句：两处读的是同一份配置，失败原因也同一批（商品不存在 /
// 库存真源端口未接入，后者拿不到可用量）。复用词条而不是另起一句，
// 免得同一件事在两个页面上给出两种说法。
// bundlePanelReadFailedMsg 上面那条提示的**事实**（片段槽位按文本渲染，见调用点注释）。
var bundlePanelReadFailedMsg = productMsg{Key: productenums.ProductBundleDetailFailed, Fallback: bundlePanelReadFailedFallback}

// bundlePanelReadFailedFallback 上述 key 的中文兜底（i18n 未初始化或词条缺失时用）。
const bundlePanelReadFailedFallback = "配置读取失败：商品不存在，或库存真源端口未接入（无法给出可用量）。"

// warehouseSKUOptionSize 已随取数下沉搬到 outbound 适配器
//（internal/module/product/outbound/inventory 的 warehouseSKUOptionSize）：
// 每仓取多少条候选是**取数口径**，与页面怎么渲染无关。

// ==========================================================================
// 捆绑接口的四个出口：读配置、存配置、整单校验、SKU 数据源。
// 整单校验在这里是**后端硬校验**：前端片段调的是同一个 service 方法，
// 因此「构造请求绕过前端」得到的结论与前台一致（验收 4）。
// ==========================================================================
// GetBundleConfig 读某商品的捆绑配置（含每项 SKU 快照与真源可用量）。
func (h *Handle) GetBundleConfig(c *gin.Context) {
	req := &productdto.GetBundleConfigReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// SetBundleConfig 保存捆绑配置（整体替换）。
func (h *Handle) SetBundleConfig(c *gin.Context) {
	req := &productdto.SetBundleConfigReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：配置变更要落主数据变更记录，操作人只能来自会话。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.SetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleSaveSuccess, res)
}

// ValidateBundleSelection 整单硬校验 + 算价（纯读，不写库）。
func (h *Handle) ValidateBundleSelection(c *gin.Context) {
	req := &productdto.ValidateBundleSelectionReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ValidateBundleSelection(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleValidateSuccess, res)
}

// ListBundleSKUs 可挑选的 SKU 清单（跨商品，后台配置器的下拉数据源）。
func (h *Handle) ListBundleSKUs(c *gin.Context) {
	req := &productdto.ListBundleSKUReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListBundleSKUs(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}
