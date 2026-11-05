package productenums

import "go_wp/pkg/facing"

const (
	ErrDetailTemplateNameRequired = "pTplNameReq"
	ErrDetailTemplatePathRequired = "pTplPathReq"
	ErrDetailTemplateUnavailable  = "pTplUnavailable"
	ErrDetailTemplatePreviewEmpty = "pTplPreviewNil"
)

func ActionDoneMessage() facing.Msg {
	return facing.Msg{Key: ProductsActionDone, Fallback: "操作已完成"}
}
