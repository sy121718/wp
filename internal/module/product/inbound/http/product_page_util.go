package producthttp

import (
	"context"
	"encoding/json"
	"go_wp/internal/middleware/builtin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/shell"
	"go_wp/pkg/money"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_page_util.go — 商品各页面共用的表单回显、分页与取值小工具。
//
// formEcho 一族是「写失败时原地留住输入」的底座：值从**本次提交**里读回，
// 所以键名要带前缀（FormEcho / FormEchoChecked / FormEchoMulti）—— 片段与页面共用
// 同一份渲染 data，通用键名会撞上页面已有键。

// formProductID 取表单里的商品 id：优先 productId，再回落 id。
//
// 两个名字都是**既有字段名**，不下令重命名：详情页的商品级表单（属性引用 / 分类与品牌 /
// 手工标签）用的隐藏域是 id（其值就是商品 id），子资源表单（变体 / 评分）用的是 productId。
// 只适用于这两者都指向商品本身的地方 —— 变体删除 / 评分删除的 id 是**子资源 id**，
// 那两个 handler 必须读 productId，绝不能走这里的回落。
// 「提交失败原地留住输入」出口（路径 A 渐进增强）。
//
// 形状与口径**照批 1 的商品建表单**（product_new_page.go + partials/product_create_form.html），
// 分档判据只有一份（product_page_util.go 的 isHXRequest / hxFragment / formEchoData）：
//
//	· htmx 请求：失败返 200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回列表页（取代原先的 302 + ?err=）；
//	· 成功出口：一律整页提示 —— htmx 由 shell.RenderJump 输出 HX-Redirect（整页跳转），
//	  原生渲染提示页。
//
// 为什么单独一个文件：属性页有三个抽屉表单（新建属性组 / 编辑属性组 / 保存属性值），
// 每个抽屉的数据既要在**页面首屏**渲染（初值 = 库里当前值），又要在**提交失败**时由
// handler 单独重渲染（值 = 用户刚打的字）。两处各拼一份 data 必然分叉，而分叉不会编译
// 失败 —— 只会让失败重渲染缺一个键，于是 Jet 在那一行渲染失败、**整块响应被丢弃（500）**，
// 而 htmx 把 5xx 判成不 swap：用户点「保存」后页面上毫无反应（比白页更难查）。
// 因此组装只有 attrGroupFormData / attrValuesFormData 两个入口，首屏与失败共用。

// redirectWhere 已提升到 product_page_util.go（同包共享，本文件仍可直接调用）。
// 提升理由：它是「HTMX 写表单分档」的出口之一，写表单的失败片段再走同一个口径，
// 两处各写一份头判断迟早会分叉。

// —— HTMX 写表单的失败分档（各写表单共用）——
//
// 表单保留原生 `method="post" action=…`，另加 `hx-post`（htmx 优先拦住 submit）；
// 服务端按 `HX-Request` **分档**：
//
//	· htmx 请求：失败返 **200 + 片段**（错误槽 + 回填后的表单）—— 抽屉 / 页面原地留住输入；
//	· 原生请求：整页提示（shell.RenderJump）回来源页（取代原先的 302 + ?err=）。
//
// 分档判据只有 isHXRequest 一处实现：两个出口必须认同同一个信号，否则会出现
// 「片段按 htmx 走、跳转按原生走」这种自相矛盾的档位。
//
// 原 redirectWhere / hxFragment 两个 helper 已删除：写动作的结论统一由 shell.RenderJump
// 渲染（它对 htmx 自己发 HX-Redirect），回填片段分支由各 handler 就地 `c.HTML` 渲染。

// product_view.go - 商品页的视图构造（变体行、规格标签、金额/评分格式化、仓库下拉
// 与捆绑构成面板）。

// ---------------------------------------------------------------------------
// 商品域后台子列表（属性 / 分类 / 品牌 / 标签）共用的分页与取址助手。
//
// 为什么落在这个文件里：本批的独占文件清单只含三个 handler 与四个模板，共享助手所在的
// product_page_util.go 不在其中（有并行任务在同包改别的页面，动它必然冲突）；同包内位置
// 不影响可用性，四页都能直接调用。
// ---------------------------------------------------------------------------
//
// 并进来的还有 product_handle.go 的 operatorFromContext（会话里的操作人）。

func formProductID(c *gin.Context) string {
	if id := strings.TrimSpace(c.PostForm("productId")); id != "" {
		return id
	}
	return strings.TrimSpace(c.PostForm("id"))
}

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 大小写不敏感、去首尾空白：与 pkg/reqparams 的请求头判据同口径。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// formEchoMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
const formEchoMemory = 32 << 20

// formEcho 这次请求的表单快照（失败片段回填用）。
//
// 值只来自**这次提交**，不回查数据库：回填要的是「用户刚打的字」，而不是「库里的旧值」
// （新建路径上后者根本不存在）。
type formEcho struct {
	// values 归一化后的提交值：去首尾空白、丢空项（与 reqparams.FieldValue 同口径）。
	// 多值字段保留**提交顺序** —— 顺序在本域有语义（「按本表顺序第一个勾中的仓是认领仓」），
	// 排序或去重都会改掉它。
	values url.Values
}

// formEchoFrom 取这次请求的表单快照。
func formEchoFrom(c *gin.Context) formEcho {
	vals := url.Values{}
	if c == nil || c.Request == nil {
		return formEcho{values: vals}
	}
	// 走标准库的解析入口（gin 的 PostForm 也走这里）：urlencoded 与 multipart 两条路径
	// 都会把字段填进 req.PostForm。解析错误一律忽略 —— 解析失败就当「没提交」，
	// 回填退化成空表单；提交本身合法与否由各 handler 的业务校验回答，不在这里变成 500。
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	for key, list := range c.Request.PostForm {
		for _, raw := range list {
			if v := strings.TrimSpace(raw); v != "" {
				vals.Add(key, v)
			}
		}
	}
	return formEcho{values: vals}
}

// value 取单值字段的回填值（缺失返回空串）：文本 / 下拉 / 隐藏域用。
func (e formEcho) value(name string) string {
	if list := e.values[name]; len(list) > 0 {
		return list[0]
	}
	return ""
}

// list 取多值字段的完整回填值（复选框组 / 同名多次提交），保留提交顺序。
func (e formEcho) list(name string) []string {
	return e.values[name]
}

// checked 该字段这次是否被提交过（复选框的回填判据）。
//
// 判据是「字段在提交里存在」：复选框只有被勾选才会提交 —— 所以勾选态用 checked，
// 文本值用 value，两者不要混用（文本框的值可能是空串，那不代表「没填过」）。
func (e formEcho) checked(name string) bool {
	return len(e.values[name]) > 0
}

// formEchoData 把回填值摊成片段 data 可直接并入的三个键。
//
//	"FormEcho"        gin.H{字段名: string}   文本 / 下拉 / 隐藏域 → value="{{ .FormEcho.sku }}"
//	"FormEchoChecked" gin.H{字段名: bool}     复选框 → {{if .FormEchoChecked.trackQuantity}}checked{{end}}
//	"FormEchoMulti"   gin.H{字段名: []string} 多值字段的完整提交值（保序）
//
// **键名必须带 FormEcho 前缀**：片段与页面共用同一份渲染 data，而页面自己的键空间里
// 早已有同名键 —— 商品列表页的 `Form` 是**批量改价的 productdto.PricingForm 结构体**
// （product_page_handle.go 的 `"Form": defaultPricingForm(...)`）。沿用裸 `Form` 会让片段里的
// `{{.Form.name}}` 命中结构体，Jet 报 `can't use name as field name in struct type` 并
// **从那一行截断整页**（HTTP 仍 200）—— 而且只在列表页那条渲染路径上炸，新建整页看不出来。
//
// **字段名要逐个列出**：Jet 读 map 里缺失的键会抛运行时错误、整个片段渲染失败 ——
// 渲染器先渲到 buffer，失败走 `http.Error(500, …)`（buffer 里的半截内容被丢弃），
// 而 htmx 默认把 5xx 判成 `swap:false`：用户点了保存**什么都看不到**。
// 所以列出的字段都被补成零值。模板里访问**未列出**的字段仍要用 isset 包裹
// （见 internal/templates/CLAUDE.md 的「可选数据键必须用 isset 判断」）。
func formEchoData(c *gin.Context, fields ...string) gin.H {
	echo := formEchoFrom(c)
	form := make(gin.H, len(fields))
	checked := make(gin.H, len(fields))
	multi := make(gin.H, len(fields))
	for _, name := range fields {
		form[name] = echo.value(name)
		checked[name] = echo.checked(name)
		multi[name] = echo.list(name)
	}
	return gin.H{"FormEcho": form, "FormEchoChecked": checked, "FormEchoMulti": multi}
}

// variantListRows 变体「清单」表的初始行 —— 直接来自**库里已有的变体**。
//
// 关键语义（docs/14 §8「预览—保存」）：清单的种子是既有变体，**不是重算笛卡尔积** ——
// 重算会让上次删掉的组合自己冒出来。只有运营再点一次「生成组合」（显式动作）才会
// 把新组合追加进清单；保存之前库里一个字节都不变，所以行上的「移除」只是移出清单。
//
// OptionValues 是**规范化后的 JSON 文本**（逐行原样回传服务端做组合去重与跳过提示），
// 规格列的可读文本由服务端拼（specLabel），前端不做属性名解析。
func variantListRows(detail *productdto.ProductResp) []gin.H {
	rows := make([]gin.H, 0, len(detail.Variants))
	for _, v := range detail.Variants {
		rows = append(rows, gin.H{
			"ID": v.ID, "SKUCode": v.SKUCode, "Spec": specLabel(v.OptionValues, detail.Attributes),
			"OptionValues": optionValuesJSON(v.OptionValues),
			"Price":        formatAmount(v.Price),
			"ComparePrice": formatNullableAmount(v.ComparePrice),
			"CostPrice":    formatNullableAmount(v.CostPrice),
			"Enabled":      v.Enabled, "StockTotal": v.StockTotal,
		})
	}
	return rows
}

// optionValuesJSON 变体组合的规范化 JSON 文本（空 / null 归一到 {}）。
//
// 它同时是清单行的 data 属性值与预览去重的入参：组合为空的对象表示「无规格变体」
// （商品创建时的首个变体），归一成 {} 而不是空串，前端 JSON.parse 才不会炸。
func optionValuesJSON(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "{}"
	}
	return s
}

// variationAttributes 参与变体的属性组（组合生成面板只勾选这些组）。
func variationAttributes(attrs []*productdto.AttributeResp) []*productdto.AttributeResp {
	out := make([]*productdto.AttributeResp, 0, len(attrs))
	for _, a := range attrs {
		if a != nil && a.IsVariation {
			out = append(out, a)
		}
	}
	return out
}

// specLabel 规格组合的可读文本：按属性组定义顺序把「组名 值名」拼起来。
//
// 组已被删除或 key 改过的历史组合用原始 key→值兜底显示，不让后台丢信息。
func specLabel(raw json.RawMessage, attrs []*productdto.AttributeResp) string {
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return "—"
	}
	used := map[string]bool{}
	parts := make([]string, 0, len(m))
	for _, a := range attrs {
		if a == nil {
			continue
		}
		value, ok := m[a.Key]
		if !ok {
			continue
		}
		used[a.Key] = true
		label := value
		for _, v := range a.Values {
			if v.Key == value {
				label = v.Label
				break
			}
		}
		parts = append(parts, a.Name+" "+label)
	}
	rest := make([]string, 0, len(m))
	for k, v := range m {
		if !used[k] {
			rest = append(rest, k+" "+v)
		}
	}
	sort.Strings(rest)
	parts = append(parts, rest...)
	return strings.Join(parts, " · ")
}

// formatAmount 数值 → 后台展示文本（整数不带小数尾巴）。
//
// 唯一实现在 pkg/money.FormatYuan（审计 CQ-013：此前与商品构建期的 formatPrice
// 逐字节重复）。保留本名字是因为同包多个后台页面文件共用它。
func formatAmount(v float64) string { return money.FormatYuan(v) }

// formatNullableAmount 可空数值 → 展示文本（空显示为 —）。
func formatNullableAmount(v *float64) string {
	if v == nil {
		return "—"
	}
	return formatAmount(*v)
}

// formatScore 评分统一两位小数（4.5 → 4.50）。
//
// 与集合源给前台的 rating 字段同一口径：评分是 0~5 的小数，
// 用最短表示会得到 4.5 / 4.25 混排，两位小数更符合评分展示习惯。
func formatScore(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// ratingOf 读某商品的评分明细与投影值（读不到就按「没有评分」处理，页面照常渲染）。
//
// projectID 由调用方给出（DB-009）：product_ratings 有 FORCE 策略，明细查询要工程作用域。
// 兜底解析（resolveProjectID 取唯一工程）在多工程下会直接判参数错，所以后台页这里把
// 自己在用的那个工程显式传下去，不走兜底。
func ratingOf(svc productcontract.ProductService, ctx context.Context, projectID, productID string) *productdto.RatingResp {
	res, err := svc.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: productID, ProjectID: projectID})
	if err != nil || res == nil {
		return &productdto.RatingResp{}
	}
	return res
}

// containsString 判断切片是否含目标值。
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// parseFloat 解析表单里的金额字段（非法值返回错误，由调用方忽略）。
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// translationFact enums key → 一条文案**事实**（不取词；取词在页面模板里做）。
//
// 这几个 key 只被商品译文工作台的**页面**消费（Errors 槽位渲染进
// admin/product/product_translations.html），没有 JSON 出口读它们 —— 所以它们不属于
// 「JSON 出口的成品串」那条例外，控制器只交 key + 中文兜底，语言由页面的 tf 决定。
// 曾经这里是 translationMsg / translationMsgs（控制器里 shell.TranslateFor 取词），
// 后果是英文后台下这几条行级结论永远是中文，且改一句话要动 Go。
//
// 空 key 与空兜底同时出现才算「没有这条文案」（模板判空写
// `{{if x.Key != "" || x.Fallback != ""}}`，结构体恒为真，不能写 `{{if x}}`）。
func translationFact(msg string) productMsg {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return productMsg{}
	}
	return productMsg{Key: msg, Fallback: productenums.TranslationMsgFallbacks[msg]}
}

// translationRowErrors 页面级错误（对应不到具体某一行）→ 事实行，顺序即发生顺序。
func translationRowErrors(keys ...string) []productdto.TranslationRowError {
	out := make([]productdto.TranslationRowError, 0, len(keys))
	for _, k := range keys {
		if fact := translationFact(k); fact.Key != "" || fact.Fallback != "" {
			out = append(out, productdto.TranslationRowError{Msg: fact})
		}
	}
	return out
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyString 取第一个非空值。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}

// productTranslateMiddleware 让本模块的 service 能按请求语言取词。
//
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），而它要产出**展示文案**：内置定价 / 标签规则的展示名与描述、筛选条件的可读
// 标签、调价行的状态与原因。这里把 shell.TranslateFor(c) 放进 Request 的 Context，
// handler 里 `ctx := c.Request.Context()` 因此自带取词函数 ——
// 既不必给每个 contract 方法加 tr 参数，也不会漏掉任何一个调用点（漏一个的表现是
// 那句话在英文界面上还是中文，且没有任何测试会红）。
//
// 未挂本中间件的路径（单测直调 service）行为不变：取词函数缺失时 service 回落中文兜底
// （见 service/product_translate.go 的 translateFrom）。
func productTranslateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tr := productservice.TranslateFunc(shell.TranslateFor(c))
		c.Request = c.Request.WithContext(productservice.WithTranslate(c.Request.Context(), tr))
		c.Next()
	}
}

// productSubListPageSize 商品域后台子列表的每页条数。
//
// 20 与商品列表（productListPageSize）同一量级：这四页的表格都带行内抽屉与批量勾选，
// 一页塞太多等于把「翻页」换成「滚动回去找刚才那一行」。
const productSubListPageSize = 20

// clampPageToTotal 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 为什么在取数**之前**收敛：越界页码（手输 URL、书签失效、上一次筛选后的页码）传给
// 取数层时，service 会老老实实返回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样产生的
// （product_list_paging_test.go 的 TestListPageSlice 钉的是同一条判据）。
//
// 与 pkg/pagination.BuildPagination 内部的收敛同一条规则（总页数由 total 与 size 算出），
// 差别只在于这里发生在取数之前。
func clampPageToTotal(page, size int, total int64) int {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(size) - 1) / int64(size))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

// listFilterQuery 列表页筛选条件的查询串（筛选表单与分页基地址共用同一份口径）。
func listFilterQuery(projectID, keyword string) url.Values {
	q := url.Values{}
	if v := strings.TrimSpace(projectID); v != "" {
		q.Set("project", v)
	}
	if v := strings.TrimSpace(keyword); v != "" {
		q.Set("keyword", v)
	}
	return q
}

// productListBaseURL 列表页的分页基地址（**不含** page/limit：分页组件自己拼）。
//
// 把筛选条件拼进基地址，翻页时关键词才不会丢；反过来说，基地址里塞了 page 就会出现两个
// page 参数（浏览器取第一个），翻页看起来「点了没反应」—— 与 productListFilterURL
// （商品列表专用，参数固定为 keyword + status）同一条理由。
func productListBaseURL(path string, q url.Values) string {
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// listPageSlice 切出「第 page 页」，并返回**收敛后**的页码。
//
// 保留旧分页助手供同包既有测试使用；分类页现走受限分类读。
//
// 页码收敛与 BuildPagination 同一条规则：page=999 时若不先收敛，会出现「表格为空、
// 分页条却显示第 999 页」这种自相矛盾的组合（BuildPagination 拿到的 total 与 page
// 不同源时就会这样）。
func listPageSlice[T any](all []T, page, size int) (rows []T, current int) {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := (len(all) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	from := (page - 1) * size
	if from > len(all) {
		from = len(all)
	}
	to := from + size
	if to > len(all) {
		to = len(all)
	}
	return all[from:to], page
}

// ==========================================================================
// 会话里的操作人（接口面与页面面共用）
// ==========================================================================
// operatorFromContext 从会话取操作人（issue #13 定价留痕 / issue #19 变更记录共用）。
//
// 优先取登录名：留痕与变更记录都要能直接读懂「谁改的」（与库存流水的 operator_id 同口径，
// 后台页面路径也一直是用登录名）。登录名缺失（脚本 / 测试路径）时退回数值 id ——
// 会话中间件写入的 user_id 是 int64，这里只做展示用的文本化，不参与任何鉴权判断。
// 两者都没有时返回空串：留痕字段允许为空。
//
// 不走 builtin.CurrentUserIDText：要保留「先登录名、后 id 文本」的组合语义，且非 int64
// 原始值分支要照旧（脚本 / 测试路径可能写入别的形状），builtin.CurrentUserID 入口只认 int64。
func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}
