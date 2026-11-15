package producthttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

func (h *ProductPageHandle) ProductCategoriesPage(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.CatalogTaxonomyReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[productdto.CatalogTaxonomyReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[productdto.CatalogTaxonomyReq](c)
	if err != nil {
		response.ParamError(c)
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
	runProductWriteJSON(c, func(ctx context.Context, req *productdto.CreateCategoryReq) (any, error) {
		return h.svc.CreateCategory(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) UpdateCategory(c *gin.Context) {
	runProductWriteJSON(c, func(ctx context.Context, req *productdto.UpdateCategoryReq) (any, error) {
		return h.svc.UpdateCategory(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) GetCategory(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.GetCategoryReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[productdto.ListCategoryReq](c)
	if err != nil {
		response.ParamError(c)
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
	runProductWriteErr(c, func(ctx context.Context, req *productdto.DeleteCategoryReq) error {
		return h.svc.DeleteCategory(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) BulkDeleteCategories(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[productdto.BulkDeleteReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	h.bulkDeleteResult(c, h.svc.BulkDeleteProductCategories, form)
}

func (h *ProductHandle) BulkDeleteBrands(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[productdto.BulkDeleteReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	h.bulkDeleteResult(c, h.svc.BulkDeleteProductBrands, form)
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
	runProductWrite(c, func(ctx context.Context, req *productdto.CreateBrandReq) (any, error) {
		return h.svc.CreateBrand(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) UpdateBrand(c *gin.Context) {
	runProductWrite(c, func(ctx context.Context, req *productdto.UpdateBrandReq) (any, error) {
		return h.svc.UpdateBrand(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) GetBrand(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.GetBrandReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[productdto.ListBrandReq](c)
	if err != nil {
		response.ParamError(c)
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
	runProductWriteErr(c, func(ctx context.Context, req *productdto.DeleteBrandReq) error {
		return h.svc.DeleteBrand(ctx, req)
	}, productWriteOK)
}
