package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

type PublishRecordReader interface {
	Langs(ctx context.Context, projectID string, entityIDs []string) (map[string][]productdto.PublishRecordLangResp, error)
}
