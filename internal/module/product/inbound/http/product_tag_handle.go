package producthttp

// product_tag.go — 标签库：手工标签与自动标签的接口面、管理页、命中片段与规则参数渲染。
//
// 标签归属是商品的属性（挂载表单在商品详情页），但它的定义、重算与命中查询是独立的一套 ——
// 与分类 / 品牌（product_taxonomy.go）分开：标签有规则注册表与重算时机，改动频率与归类树完全不同。

import (
	"errors"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"

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
func (h *ProductPageHandle) ProductTagsPage(c *gin.Context) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	req, ok := binding.Query[productdto.ProductTagsPageReq](c)
	if !ok {
		return
	}
	view, err := reader.ProductTagsPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	if view == nil {
		pageview.Fail(c, errors.New("页面读模型返回空视图"))
		return
	}

	// 行与两个抽屉的初值都在读模型里装好（TagPageRows / TagCreateForm）；筛选上下文作为请求
	// 事实由入口塞进入参，读模型直接搬进视图。
	pageview.Render(c, view)
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
func (h *ProductPageHandle) ProductTagHitsFragment(c *gin.Context) {
	req, ok := binding.Query[productdto.ListTagProductsPageReq](c)
	if !ok {
		return
	}
	res, err := h.products.ListTagProductsPage(c.Request.Context(), req)
	// 行、总数与分页事实的形状在 service（纯函数）：控制器只把取回来的那一页交出去。
	data, cause := productservice.TagHitsFragmentData(req, res, err)
	if cause != nil {
		data["Err"] = response.MessageOf(c, "product", cause)
	}
	// 行、总数与分页事实都由读模型摊好（TagHitsFragmentData）：命中商品行就是 service 的
	// TagProductResp，模板读 p.Name/p.Slug/p.Status；片段不按 HasPagination 设闸
	// （单页非空时也要渲染两个禁用按钮，「还有没有下一页」是页面上可读的事实）。
	pageview.Render(c, data)
}
