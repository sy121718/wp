package producthttp

// product_tag.go — 标签库：手工标签与自动标签的接口面、管理页、命中片段与规则参数渲染。
//
// 标签归属是商品的属性（挂载表单在商品详情页），但它的定义、重算与命中查询是独立的一套 ——
// 与分类 / 品牌（product_taxonomy_handle.go）分开：标签有规则注册表与重算时机，改动频率与归类树完全不同。

import (
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

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

func (h *ProductPageHandle) ProductTagsPage(c *gin.Context) {
	req, ok := binding.Query[productdto.CatalogTaxonomyReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	req.ProjectID = scope.ProjectID
	result, err := h.products.QueryTags(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	rules := h.products.ListTagRuleTypes(c.Request.Context())
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result), pageview.Object("RuleTypes", rules)))
}

// ProductTagHitsFragment 只读命中结果交原对象，失败由统一 JSON 出口给真实状态码。
func (h *ProductPageHandle) ProductTagHitsFragment(c *gin.Context) {
	req, ok := binding.Query[productdto.ListTagProductsPageReq](c)
	if !ok {
		return
	}
	result, err := h.products.ListTagProductsPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Query", req), pageview.Object("Result", result)))
}
