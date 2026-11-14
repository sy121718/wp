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

func (h *ProductPageHandle) ProductAttributesPage(c *gin.Context) {
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
	result, err := h.products.QueryAttributes(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}

	pageview.RenderTemplate(c, ProductAttributesPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result)))
}

func (h *ProductHandle) CreateAttribute(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := bindJSONFact[productdto.CreateAttributeReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.CreateAttribute(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) UpdateAttribute(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := bindJSONFact[productdto.UpdateAttributeReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.UpdateAttribute(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) SetAttributeValues(c *gin.Context) {
	runProductWriteJSON(c, func(ctx context.Context, req *productdto.SetAttributeValuesReq) (any, error) {
		return h.svc.SetAttributeValues(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) GetAttribute(c *gin.Context) {
	req, ok := binding.Query[productdto.GetAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

func (h *ProductHandle) ListAttributes(c *gin.Context) {
	req, ok := binding.Query[productdto.ListAttributeReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListAttributes(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

func (h *ProductHandle) DeleteAttribute(c *gin.Context) {
	runProductWriteErr(c, func(ctx context.Context, req *productdto.DeleteAttributeReq) error {
		return h.svc.DeleteAttribute(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) BulkDeleteAttributes(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[productdto.BulkDeleteReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	h.bulkDeleteResult(c, h.svc.BulkDeleteProductAttributes, form)
}
