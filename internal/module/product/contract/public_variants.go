package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

type PublicVariantReader interface {
	PublicVariants(context.Context, *productdto.PublicVariantsReq) ([]productdto.PublicVariantResp, error)
}
