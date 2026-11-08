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

func (h *ProductHandle) CreateTag(c *gin.Context) {
	runProductWriteJSON(c, func(ctx context.Context, req *productdto.CreateTagReq) (any, error) {
		return h.svc.CreateTag(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) UpdateTag(c *gin.Context) {
	runProductWriteJSON(c, func(ctx context.Context, req *productdto.UpdateTagReq) (any, error) {
		return h.svc.UpdateTag(ctx, req)
	}, productWriteOK)
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
	runProductWriteErr(c, func(ctx context.Context, req *productdto.DeleteTagReq) error {
		return h.svc.DeleteTag(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) RecalcTags(c *gin.Context) {
	runProductWrite(c, func(ctx context.Context, req *productdto.RecalcTagsReq) (any, error) {
		return h.svc.RecalcTags(ctx, req)
	}, productWriteOK)
}

func (h *ProductHandle) BulkDeleteTags(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form productdto.BulkDeleteReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	h.bulkDeleteResult(c, h.svc.BulkDeleteProductTags, &form)
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
