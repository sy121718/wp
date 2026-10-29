package producthttp

// 发布记录页面分别传递工程范围、查询条件和发布业务对象。
// 重新发布复用既有 JSON 写入口，由模板声明前端行为。

import (
	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
)

// ProductPublishRecordPage GET /admin/products/publish-records：商品的当前发布状态一览。
func (h *ProductPageHandle) ProductPublishRecordPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductPublishRecordPageReq](c)
	if !ok {
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
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Records", result)))
}
