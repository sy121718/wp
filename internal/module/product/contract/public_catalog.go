package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

type PublicCatalogReader interface {
	PublicCatalog(context.Context, *productdto.PublicCatalogReq) (*productdto.PublicCatalogResp, error)
}
