package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

type ProductPageReader interface {
	QueryDetailTemplates(context.Context, string) ([]DetailTemplateCandidateView, error)
	QueryDefaultDetailTemplate(context.Context, string) (string, error)
	QueryBoundDetailTemplate(context.Context, string, string) (BoundTemplateView, error)
	ProductDetailPath(context.Context, string, *productdto.ProductResp) string
	QueryPublishRecords(context.Context, *productdto.ProductPublishRecordPageReq) (*PublishRecordList, error)
	ProductPublishAttempts(context.Context, *productdto.PublishAttemptListReq) (*productdto.PublishAttemptListResp, error)
}

type PublishRecordList struct {
	Available bool
	Records   []productdto.PublishRecordRowResp
	Total     int64
	Page      int
	Limit     int
}
