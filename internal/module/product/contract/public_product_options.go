package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

type PublicProductOptionsReader interface {
	PublicProductOptions(context.Context, *productdto.PublicProductOptionsReq) (*productdto.PublicProductOptionsResp, error)
}
