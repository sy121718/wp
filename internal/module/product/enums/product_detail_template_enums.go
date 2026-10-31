package productenums

import "go_wp/pkg/facing"

const (
	ErrDetailTemplateNameRequired = "product.err.detailTemplateNameRequired"
	ErrDetailTemplatePathRequired = "product.err.detailTemplatePathRequired"
	ErrDetailTemplateUnavailable  = "product.err.detailTemplateUnavailable"
	ErrDetailTemplatePreviewEmpty = "product.err.detailTemplatePreviewEmpty"
)

func ActionDoneMessage() facing.Msg {
	return facing.Msg{Key: ProductsActionDone, Fallback: "操作已完成"}
}
