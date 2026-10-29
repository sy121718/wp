package producthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

func (h *ProductPageHandle) ProductCategoriesPage(c *gin.Context) {
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
	result, err := h.products.QueryCategories(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	categories, err := h.products.CategoryNodes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result), pageview.Object("Categories", categories)))
}

// ProductCategoryParents 返回父级候选原对象，按钮与层级缩进由前端生成。
func (h *ProductHandle) ProductCategoryParents(c *gin.Context) {
	req, ok := binding.Query[productdto.CatalogTaxonomyReq](c)
	if !ok {
		return
	}
	result, err := h.svc.SearchCategoryParents(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, result)
}
func (h *ProductPageHandle) ProductBrandsPage(c *gin.Context) {
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
	result, err := h.products.QueryBrands(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}

	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result)))
}

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
// 权限点各自独立、成功只回一条 JSON 回执。所以只有一个入口 bulkDeleteByIDs，
// 差别只在传给用例的**方法值**。
//
// 抽一个入口的理由只剩「同口径」：四个端点共用一段「绑请求 → 调用例 → 写响应」，
// 差别只在传给用例的**方法值**。归一与上限、空勾选判定、整批拒绝的明细都在 service ——
// 入口不做计算（判据见 internal/module/CLAUDE.md 的控制器形状那条）。

// BulkDeleteCategories 批量删除分类（JSON API）。
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
