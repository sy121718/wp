package producthttp

// product_publish_record_handle.go — 商品发布记录页的控制器（B9）。
//
// 三动作：绑参数 → 调读模型 → 返回视图（取数编排、过滤与分页全在
// service/product_publish_record_page.go）。整页 GET 的失败出口只有末尾那一次
// pageview.Fail：状态码在错误声明处，文案由出口按请求语言取一次。
//
// 「重新发布」没有本文件的写入口：它复用既有的 POST /api/product/detail-template/apply
// （data-write="custom" + data-write-reload，字段 projectId / productId / templateId），
// 模板里直接声明。再写一条页面 POST 等于给同一个动作两个权限点。

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
	view, err := h.reader.ProductPublishRecordPage(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, view)
}
