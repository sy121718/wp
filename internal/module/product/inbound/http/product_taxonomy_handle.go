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
	pageview.RenderTemplate(c, ProductCategoriesPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result), pageview.Object("Categories", categories)))
}

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

	pageview.RenderTemplate(c, ProductBrandsPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result)))
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

func (h *ProductHandle) BulkDeleteCategories(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductCategories)
}

func (h *ProductHandle) BulkDeleteBrands(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductBrands)
}

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
