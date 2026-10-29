package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

// PublicVariantReader 访客可读取的商品事实；接口形状不包含内部成本快照。
type PublicVariantReader interface {
	PublicVariants(context.Context, *productdto.PublicVariantsReq) ([]productdto.PublicVariantResp, error)
}
