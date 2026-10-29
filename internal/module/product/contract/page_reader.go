package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

// ProductPageReader 商品发布与模板的业务读契约；不返回整屏数据。
type ProductPageReader interface {
	QueryDetailTemplates(context.Context, string) ([]DetailTemplateCandidateView, error)
	QueryDefaultDetailTemplate(context.Context, string) (string, error)
	QueryBoundDetailTemplate(context.Context, string, string) (BoundTemplateView, error)
	ProductDetailPath(context.Context, string, *productdto.ProductResp) string
	QueryPublishRecords(context.Context, *productdto.ProductPublishRecordPageReq) (*PublishRecordList, error)
	ProductPublishAttempts(context.Context, *productdto.PublishAttemptListReq) (*productdto.PublishAttemptListResp, error)
}

// PublishRecordList 按商品分页的发布登记；总数与筛选后集合口径相同。
type PublishRecordList struct {
	Available bool
	Records   []productdto.PublishRecordRowResp
	Total     int64
	Page      int
	Limit     int
}
