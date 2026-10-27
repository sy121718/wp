package producthttp

// product_publish_attempt_handle.go — 发布历史面板的控制器（迁移 673）。
//
// 交 **JSON 而不是 HTML 片段**：DOM 由发布记录页里已渲染的 Jet <template> 填充
//（internal/templates/static/js/ui/json_swap.js 的同源机制，见 templates/CLAUDE.md）。
// 因此这里没有模板名、没有 adminui、没有 c.HTML —— 只有「绑参数 → 调读模型 → 交事实」。
//
// 为什么挂接口组（authorizedAPI）而不是页面组：出口必须是 {code,message,data}，
// 而页面组的出口是 Jet；它也不是任何页面的导航目标（不产生菜单 path）。

import (
	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

// PublishAttemptHandle 发布历史片段处理器。
//
// 只持读模型（不是 ProductService）：这一条只读、且不需要任何写能力 —— 拿不到写方法的
// 处理器不会长出第二段业务逻辑。
type PublishAttemptHandle struct {
	reader productcontract.ProductPageReader
}

// NewPublishAttemptHandle 构造。
//
// 实参给 ProductService 也能进来：SetupProductRoutes 的 `return svc, svc` 已证明同一个值
// 的方法集覆盖 ProductPageReader（编译期成立），所以这里不写请求期断言，
// 也就没有「断言失败 → 静默降级」这条路径。
func NewPublishAttemptHandle(reader productcontract.ProductPageReader) *PublishAttemptHandle {
	return &PublishAttemptHandle{reader: reader}
}

// ProductPublishAttempts GET /api/product/publish-attempts：某商品最近的发布尝试。
//
// 未装配（台账端口缺失）不是失败：200 + ready=false，面板据此说「没接上」——
// 与「读不到」「一次都没发过」是三档。
func (h *PublishAttemptHandle) ProductPublishAttempts(c *gin.Context) {
	req, ok := binding.Query[productdto.PublishAttemptListReq](c)
	if !ok {
		return
	}
	res, err := h.reader.ProductPublishAttempts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 500, "product", err)
		return
	}
	response.Success(c, res)
}
