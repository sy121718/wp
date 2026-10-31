package producthttp

import (
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

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

func (h *ProductHandle) ListTagRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListTagRuleTypes(c.Request.Context()))
}

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

func (h *ProductHandle) BulkDeleteTags(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductTags)
}

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
	pageview.RenderTemplate(c, ProductTagsPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result), pageview.Object("RuleTypes", rules)))
}
