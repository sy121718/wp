package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// PublicProductOptionsReader 只读已上架商品的可选规格，供编辑器选择与构建期绑定复用。
type PublicProductOptionsReader interface {
	PublicProductOptions(context.Context, *productdto.PublicProductOptionsReq) (*productdto.PublicProductOptionsResp, error)
}
