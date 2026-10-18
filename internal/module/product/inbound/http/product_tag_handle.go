package producthttp

// product_tag.go — 标签库：手工标签与自动标签的接口面、管理页、命中片段与规则参数渲染。
//
// 标签归属是商品的属性（挂载表单在商品详情页），但它的定义、重算与命中查询是独立的一套 ——
// 与分类 / 品牌（product_taxonomy.go）分开：标签有规则注册表与重算时机，改动频率与归类树完全不同。

import (
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 标签页的筛选上下文键表：渲染时拼进表单 action，提交回来时按同一份表读回。
var productTagsBackKeys = []string{"project", "keyword", "page", "limit"}

// ==========================================================================
// 标签的接口面：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：规则类型白名单、参数校验与重算时机一律在 service。
// ==========================================================================

func (h *ProductHandle) CreateTag(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateTagReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateTag 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
func (h *ProductHandle) UpdateTag(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateTagReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetTag 标签详情（含命中商品列表）。
func (h *ProductHandle) GetTag(c *gin.Context) {
	req, ok := binding.Query[productdto.GetTagReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListTags 标签列表（工程内；每个标签带当前归属数量）。
func (h *ProductHandle) ListTags(c *gin.Context) {
	req, ok := binding.Query[productdto.ListTagReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagProducts 某标签命中的商品（验收 4）。
func (h *ProductHandle) ListTagProducts(c *gin.Context) {
	req, ok := binding.Query[productdto.ListTagProductsReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListTagProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagRuleTypes 内置规则类型清单（后台规则下拉的唯一来源）。
func (h *ProductHandle) ListTagRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListTagRuleTypes(c.Request.Context()))
}

// DeleteTag 删除标签（连同它在商品上的引用一起解绑）。
func (h *ProductHandle) DeleteTag(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteTagReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteTag(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// RecalcTags 手动触发重算（重算时机之一：可按标签或按工程整体重算）。
func (h *ProductHandle) RecalcTags(c *gin.Context) {
	req, ok := binding.JSON[productdto.RecalcTagsReq](c)
	if !ok {
		return
	}
	res, err := h.svc.RecalcTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// CreateCategory 新建分类（parentId 为空即顶级）。

func (h *ProductHandle) BulkDeleteTags(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductTags)
}

// bulkDeleteByIDs 四套归类（分类 / 品牌 / 标签 / 属性组）的共用入口：解析 → 归一 → 调用例 → 映射回执。
//

// ==========================================================================
// 标签库（手工标签 + 自动标签）：列表、命中片段、抽屉与批量改挂。
//
// 自动标签的命中来自规则重算（ProductTagsRecalc）；命中预览是 HTMX 片段，
// 与列表共用同一份规则参数渲染（ruleParamText）。
//
// 与本模块其它后台页同一模式：GET 渲染完整页，只用 GET/POST。三处差异：
//
//	· 建手工标签并挂到商品（挂载表单在商品**详情页**的「商品标签」区块 ——
//	  标签归属是商品的属性，列表页只回答「有哪些商品」，见 admin-ui-logic §1）；
//	· 自动标签只给内置规则类型 + 白名单参数（规则类型下拉来自 service 的注册表，
//	  参数输入框固定三格：days / minPrice / maxPrice，服务端按规则类型取值并严格校验）；
//	· 标签列表用标准表格（标签 / URL 段 / 类型 / 规则 / 命中 / 重算时间 / 操作），
//	  规则标签额外显示规则描述与重算时间；「命中的商品」按需加载（审计 PERF-02）：
//	  首屏只给数量，展开某个标签才按页取片段，见 ProductTagHitsFragment。
//
// 重算时机在页面上写明（商品/变体写操作后、标签定义变更后、这里的「重算」按钮）。
// ==========================================================================

// ProductTagsPage 标签管理页：工程切换 + 筛选栏 + 新建表单 + 规则类型说明 + 标签列表（含命中商品）。
func (h *ProductPageHandle) ProductTagsPage(c *gin.Context) (any, error) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	req, ok := binding.Query[productdto.ProductTagsPageReq](c)
	if !ok {
		return nil, response.ErrHandled
	}
	view, err := reader.ProductTagsPage(c.Request.Context(), req)
	if view == nil {
		view = &productdto.ProductTagsPageResp{
			Title: productenums.ProductTagsTitle, Menu: "product-tags",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// —— 控制器负责的部分：行装配与两个标签抽屉（要 csrf 与取词函数）——
	view.Title = productenums.ProductTagsTitle
	view.Menu = "product-tags"
	view.ListQuery = binding.ListQuery(c, productTagsBackKeys...)
	rows := []productdto.TagRowResp{}
	tagCreateForm := map[string]any{}
	if err == nil {
		rows = make([]productdto.TagRowResp, 0, len(view.TagDetails))
		for _, tg := range view.TagDetails {
			row := tagPageRow(tg)
			row.EditForm = h.tagDrawerData(c, "update", view.SelectedProject, &row, view.RuleTypes)
			rows = append(rows, row)
		}
		// 新建抽屉的片段数据与行级片段同源：同一份规则清单、同一份模板。
		tagCreateForm = h.tagDrawerData(c, "create", view.SelectedProject, nil, view.RuleTypes)
	}
	view.Tags = rows
	view.TagCreateForm = tagCreateForm
	if pg := pagination.Facts(view.Total, view.Page, view.Limit); pg.HasPagination() {
		pg.Base = pagination.BaseURL("/admin/product-tags", pagination.Query("project", view.SelectedProject, "keyword", view.FilterKeyword))
		view.Pagination = pg
	}
	if err != nil {
		view.LoadFailed = true
		view.LoadError = response.MessageOf(c, "product", err)
	}
	return view, nil
}

// ProductTagHitsFragment 标签「命中商品」片段（审计 PERF-02 的展开区）。
//
// 为什么按需取：标签页首屏此前对每个标签取一次命中商品（GetTag），SQL 条数与标签数
// 成正比，且命中数据（每标签最多 500 行）会一起撑大页面。现在首屏只给数量，
// 展开某个标签时才发这一组查询（标签存在性 + 总数 + 本页行），与页面上有多少标签无关。
//
// 归属：挂在后台**页面组**（Session + CSRF），不叠加 Casbin 权限点 —— 与同组的
// /products/seo-score、/products/variant/preview 同一先例：它是只读渲染，不落任何库
// （写面仍然逐个挂 Casbin），因此不需要新权限点，也不会出现「有路由无权限点 ⇒ 含超管
// 全员 403」。渲染出的商品行只读，行内没有任何写入口。
//
// 失败也回 200 的片段（不是整页错误页）：它替换的是页面里的一小块，
// 回整页 HTML 会把展开区之外的内容一起换掉；错误文案过 productErrText 白名单。
func (h *ProductPageHandle) ProductTagHitsFragment(c *gin.Context) (any, error) {
	req, ok := binding.Query[productdto.ListTagProductsPageReq](c)
	if !ok {
		return nil, response.ErrHandled
	}
	data := gin.H{
		"TagID":     req.TagID,
		"ProjectID": req.ProjectID,
	}
	res, err := h.products.ListTagProductsPage(c.Request.Context(), req)
	if err != nil {
		data["Err"] = response.MessageOf(c, "product", err)
		return data, nil
	}
	data["TagName"] = res.TagName
	data["Total"] = res.Total
	data["Page"] = res.Page
	data["PageSize"] = res.PageSize
	data["TotalPage"] = res.TotalPage
	// 命中商品行就是 service 的 TagProductResp：模板读 p.Name/p.Slug/p.Status，
	// 字段名与它一致，控制器不再逐行翻译一份（那些翻译只会与 service 漂移）。
	data["Items"] = res.Items
	// 分页交事实（总数 / 页码 / 每页条数 / 基地址），文案与翻页链接由
	// admin/product/product_tag_hits.html 用同一套通用模板函数渲染。
	//
	// 与五个整页站点的差别（有意为之）：这里**不**按 HasPagination() 设闸 —— 片段在
	// 单页非空时仍要渲染两个禁用按钮，「还有没有下一页」是页面上可读的事实；
	// 闸门只由模板的「有没有行」决定。
	//
	// KeepLimit=false：本端点只认 project/id/page/size 四个查询参数，翻页链接里不带 limit
	//（旧 tagHitsURL 也不带）。Base 用**请求里的** project 值（不是 service 解析后的工程）：
	// 链接必须原样回带用户点进来时的那一组参数，空值也保留成 `project=`。
	// 键序 id → page → project 与旧实现的 url.Values.Encode() 逐字节一致（字典序）。
	pg := pagination.Facts(int64(res.Total), res.Page, res.PageSize)
	pg.Base = "/admin/product-tags/hits"
	pg.PageParam = "page"
	pg.KeepLimit = false
	data["Pagination"] = pg
	return data, nil
}

// tagDrawerData 装配标签编辑片段数据（create 时 row 为 nil）。
// ruleTypes 由调用方传入：页面装配时取一次全行共享，失败分支自己取一次，
// 不在片段装配里重复查契约。取词走 text.Inject（片段不经整份
// Prepare，但模板要 t/tf；漏注入不报错 —— 取词全变空串、200、日志干净）。
func (h *ProductPageHandle) tagDrawerData(c *gin.Context, mode, projectID string, row *productdto.TagRowResp, ruleTypes []*productdto.TagRuleTypeResp) map[string]any {
	data := map[string]any{
		"Mode": mode, "Project": projectID,
		"RuleTypes": ruleTypes,
		"ID":        "", "Name": "", "Slug": "", "IsRule": false, "Sort": 0,
		"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
	}
	// 逐字段搬而不是按键名遍历：行类型改了名字这里会编译失败，键名写错则只有
	// 模板里静默少一段初值。
	if row != nil {
		data["ID"], data["Name"], data["Slug"] = row.ID, row.Name, row.Slug
		data["IsRule"], data["Sort"], data["RuleType"] = row.IsRule, row.Sort, row.RuleType
		data["RuleDays"], data["RuleMinPrice"], data["RuleMaxPrice"] = row.RuleDays, row.RuleMinPrice, row.RuleMaxPrice
	}
	return text.Inject(c, data)
}

// tagPageRow 标签 → 模板行（规则描述与命中数都由服务端算好，模板不做第二套解释；
// 命中商品本身不在这里，展开时才由 ProductTagHitsFragment 给 —— 审计 PERF-02）。
//
// 文案槽给**事实**（key + 中文兜底），取词在 product_tags.html 的 tr 里做：控制器不取词。
// RecalcAt 是**数据**（已格式化的时间串，未重算时为空），「未重算过」那句话才是文案。
func tagPageRow(t *productdto.TagResp) productdto.TagRowResp {
	// 类型 → 文案事实的映射在 productenums.TagKindLabel（控制器不再挑文案）。
	kindKey, kindFallback := productenums.TagKindLabel(t.Kind)
	recalcAt, recalcKey, recalcFallback := recalcFact(t.RecalcAt)
	return productdto.TagRowResp{
		ID: t.ID, Name: t.Name, Slug: t.Slug, Kind: t.Kind,
		KindLabelKey: kindKey, KindLabelFallback: kindFallback,
		IsRule:        t.Kind == productenums.TagKindRule,
		HasRuleParams: t.Kind == productenums.TagKindRule,
		KindTone:      tagKindTone(t.Kind),
		KindRule:      productenums.TagKindRule,
		KindManual:    productenums.TagKindManual,
		RuleType:      t.RuleType,
		RuleLabel:     t.RuleLabel,
		RecalcAt:      recalcAt, RecalcLabelKey: recalcKey, RecalcLabelFallback: recalcFallback,
		ProductCount: t.ProductCount, Sort: t.Sort,
		RuleDays:     ruleParamText(t.RuleParams, "days"),
		RuleMinPrice: ruleParamText(t.RuleParams, "minPrice"),
		RuleMaxPrice: ruleParamText(t.RuleParams, "maxPrice"),
	}
}

// tagKindTone 标签类型 → 徽标分档（规则型标签 → info，普通标签中性）。
func tagKindTone(kind string) string {
	if kind == productenums.TagKindRule {
		return "info"
	}
	return "mute"
}

// recalcFact 重算时间 → （数据串, 文案事实）。
//
// 输入是 RFC3339，这里压成「2006-01-02 15:04」：既好读，又不会因为那一长串
// 时间戳在窄屏折叠行里连成不可断行的 token 把卡片撑破。
// 时间是数据（格式固定，走第一个返回值），只有「没重算过」那句话是文案 ——
// 给 key + 中文兜底（productenums.TagRecalcLabel），取词在 product_tags.html 的 tr 里做。
func recalcFact(at string) (label, key, fallback string) {
	key, fallback = productenums.TagRecalcLabel(at)
	return recalcAtText(at), key, fallback
}

// recalcAtText 重算时间的数据串：RFC3339 → 「2006-01-02 15:04」。
//
// 空值给空串（模板据它渲染「—」那一档），不可解析时**原样返回** —— 至少把原值显示出来，
// 留白会让人以为「没有这个数据」。
func recalcAtText(at string) string {
	at = strings.TrimSpace(at)
	if at == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// ruleParamText 从规则参数里取某个键的文本（缺失返回空串，用于表单回填）。
func ruleParamText(params json.RawMessage, key string) string {
	if len(params) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	return s
}
