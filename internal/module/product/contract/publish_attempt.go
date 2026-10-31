package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

type PublishAttemptReader interface {
	PublishAttempts(ctx context.Context, projectID, productID string, limit int) (res *productdto.PublishAttemptListResp, err error)
}
