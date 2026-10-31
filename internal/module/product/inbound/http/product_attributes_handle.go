package producthttp

import (
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
	req, ok := binding.JSON[productdto.CreateAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

func (h *ProductHandle) UpdateAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

func (h *ProductHandle) SetAttributeValues(c *gin.Context) {
	req, ok := binding.JSON[productdto.SetAttributeValuesReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SetAttributeValues(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
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
	req, ok := binding.JSON[productdto.DeleteAttributeReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteAttribute(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

func (h *ProductHandle) BulkDeleteAttributes(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductAttributes)
}
