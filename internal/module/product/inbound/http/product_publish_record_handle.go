package producthttp

import (
	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

func (h *ProductPageHandle) ProductPublishRecordPage(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.ProductPublishRecordPageReq](c)
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
	result, err := h.reader.QueryPublishRecords(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, ProductPublishRecordPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Records", result)))
}
