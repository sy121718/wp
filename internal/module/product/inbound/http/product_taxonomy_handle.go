package producthttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

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
	c.Header("Cache-Control", "no-store")
	var req productdto.CreateCategoryReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.CreateCategory(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) UpdateCategory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.UpdateCategoryReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.UpdateCategory(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
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
	c.Header("Cache-Control", "no-store")
	var req productdto.DeleteCategoryReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) BulkDeleteCategories(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form productdto.BulkDeleteReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	h.bulkDeleteResult(c, h.svc.BulkDeleteProductCategories, &form)
}

func (h *ProductHandle) BulkDeleteBrands(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form productdto.BulkDeleteReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	h.bulkDeleteResult(c, h.svc.BulkDeleteProductBrands, &form)
}

// bulkDeleteResult 是批量删除的公共出口：调用方已经完成请求绑定。
func (h *ProductHandle) bulkDeleteResult(c *gin.Context, del func(context.Context, *productdto.BulkDeleteReq) (*productdto.BulkResultResp, error), req *productdto.BulkDeleteReq) {
	if _, err := del(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) CreateBrand(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.CreateBrandReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.CreateBrand(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) UpdateBrand(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.UpdateBrandReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.UpdateBrand(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
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
	c.Header("Cache-Control", "no-store")
	var req productdto.DeleteBrandReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if err := h.svc.DeleteBrand(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}
