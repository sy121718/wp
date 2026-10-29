package productenums

import "go_wp/pkg/facing"

const (
	ErrDetailTemplateNameRequired = "product.err.detailTemplateNameRequired"
	ErrDetailTemplatePathRequired = "product.err.detailTemplatePathRequired"
	ErrDetailTemplateUnavailable  = "product.err.detailTemplateUnavailable"
	ErrDetailTemplatePreviewEmpty = "product.err.detailTemplatePreviewEmpty"
)

// ActionDoneMessage 是商品写动作的成功事实；取词由响应出口完成。
func ActionDoneMessage() facing.Msg {
	return facing.Msg{Key: ProductsActionDone, Fallback: "操作已完成"}
}
