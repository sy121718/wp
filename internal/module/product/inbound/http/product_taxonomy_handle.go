package producthttp

import (
	"context"
	"errors"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

// product_taxonomy.go — 商品的两套「归类」：分类树与品牌（接口面 + 后台页面）。
//
// 两者在界面上分属两个菜单页，但它们是同一类东西：都只被商品**引用**、都参与筛选、
// 都有 slug / 引用守卫 / 批量删除，改一处口径另一处必须跟着看。
// 标签是第三种归类，但它有规则注册表与重算时机、改动频率完全不同，所以单独一份（product_tag.go）。

// 两页的筛选上下文键表：渲染时拼进表单 action，提交回来时按同一份表读回。
var (
	productCategoriesBackKeys = []string{"project", "keyword", "page", "limit"}
	productBrandsBackKeys     = []string{"project", "keyword", "page", "limit"}
)

func (h *ProductPageHandle) ProductCategoriesPage(c *gin.Context) {
	// 读模型在装配期注入（见 ProductPageHandle.reader）：漏接 = 编译错误，
	// 不再有「请求期断言失败 → 500 兜底」这条路径。
	reader := h.reader
	// 取参数交给 binding.Query，取数编排在 service（取哪些、按什么顺序、哪条失败算 400 /
	// 哪条只降级，都是业务结论。见 service.ProductCategoriesPage）。
	req, ok := binding.Query[productdto.ProductCategoriesPageReq](c)
	if !ok {
		return
	}
	req.ListQuery = binding.ListQuery(c, productCategoriesBackKeys...)
	view, err := reader.ProductCategoriesPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	if view == nil {
		pageview.Fail(c, errors.New("页面读模型返回空视图"))
		return
	}

	// 候选下拉、新建抽屉与行都在读模型里装好（CategoryPickOptions / CategoryDrawerForm /
	// CategoryTreeRows）；入口只把「回到本页的筛选上下文」这条请求事实填进入参。
	pageview.Render(c, view)
}

// ProductCategoryParents is a bounded, project-scoped parent picker search.
//
// 工程存在性判定、关键词为空给空候选、候选形状（PickOptionResp）全在 service 的
// CategoryParentOptions 里 —— 这一层只做绑定与转交。
func (h *ProductPageHandle) ProductCategoryParents(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductCategoryParentsReq](c)
	if !ok {
		return
	}
	data, err := h.reader.CategoryParentOptions(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, data)
}

// ProductBrandsPage 品牌管理页：工程切换 + 筛选栏 + 品牌列表 + 内联新建表单。
func (h *ProductPageHandle) ProductBrandsPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductBrandsPageReq](c)
	if !ok {
		return
	}
	req.ListQuery = binding.ListQuery(c, productBrandsBackKeys...)
	view, err := h.reader.ProductBrandsPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	// 行在读模型里装好（BrandPageRows）；入口只填请求事实 ListQuery。
	pageview.Render(c, view)
}

// ==========================================================================

func (h *ProductHandle) CreateCategory(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateCategoryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateCategory 修改分类（改名 / 换父级 / 排序 / SEO 字段）。
func (h *ProductHandle) UpdateCategory(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateCategoryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetCategory 分类详情。
func (h *ProductHandle) GetCategory(c *gin.Context) {
	req, ok := binding.Query[productdto.GetCategoryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListCategories 分类树。
func (h *ProductHandle) ListCategories(c *gin.Context) {
	req, ok := binding.Query[productdto.ListCategoryReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListCategories(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteCategory 删除分类（有子级或被商品引用时拒绝）。
func (h *ProductHandle) DeleteCategory(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteCategoryReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// —— 批量删除（JSON 端点）——
//
// 四套归类（分类 / 品牌 / 标签 / 属性组）的批量删除形状逐字相同：身体是 {ids, projectId}、
// 权限点复用各自的单删、成功只回一条 JSON 回执。所以只有一个入口 bulkDeleteByIDs，
// 差别只在传给用例的**方法值**。
//
// 抽一个入口的理由只剩「同口径」：四个端点共用一段「绑请求 → 调用例 → 写响应」，
// 差别只在传给用例的**方法值**。归一与上限、空勾选判定、整批拒绝的明细都在 service ——
// 入口不做计算（判据见 internal/module/CLAUDE.md 的控制器形状那条）。

// BulkDeleteCategories 批量删除分类（页面 /admin/product-categories/bulk-delete 的承接方）。
func (h *ProductHandle) BulkDeleteCategories(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductCategories)
}

// BulkDeleteBrands 批量删除品牌。
func (h *ProductHandle) BulkDeleteBrands(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductBrands)
}

// **超限整批拒绝，不静默截断** —— 截断会让操作者以为「勾的全删了」，而实际只删了前 200 条。
func (h *ProductHandle) bulkDeleteByIDs(c *gin.Context, del func(context.Context, *productdto.BulkDeleteReq) (*productdto.BulkResultResp, error)) {
	req, ok := binding.JSON[productdto.BulkDeleteReq](c)
	if !ok {
		return
	}
	res, err := del(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, res)
}

// CreateBrand 新建品牌。
func (h *ProductHandle) CreateBrand(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateBrandReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateBrand 修改品牌。
func (h *ProductHandle) UpdateBrand(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateBrandReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetBrand 品牌详情。
func (h *ProductHandle) GetBrand(c *gin.Context) {
	req, ok := binding.Query[productdto.GetBrandReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListBrands 品牌列表。
func (h *ProductHandle) ListBrands(c *gin.Context) {
	req, ok := binding.Query[productdto.ListBrandReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListBrands(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteBrand 删除品牌（被商品引用时拒绝）。
func (h *ProductHandle) DeleteBrand(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteBrandReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteBrand(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}
