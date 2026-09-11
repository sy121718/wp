// Package productcontract product 模块对外契约（issue #5）。
package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// ProductService 商品管理契约。
//
// 只暴露商品与变体的管理能力。库存、采购、订单、客户均由各自模块负责，
// 商品侧不反向依赖它们（跨模块只走 contract）。
type ProductService interface {
	Create(ctx context.Context, req *productdto.CreateReq) (res *productdto.ProductResp, err error)
	Update(ctx context.Context, req *productdto.UpdateReq) (res *productdto.ProductResp, err error)
	Get(ctx context.Context, req *productdto.GetReq) (res *productdto.ProductResp, err error)
	List(ctx context.Context, req *productdto.ListReq) (list []*productdto.ProductResp, err error)
	Delete(ctx context.Context, req *productdto.DeleteReq) (err error)

	CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (res *productdto.VariantResp, err error)
	UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (res *productdto.VariantResp, err error)
	DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) (err error)
}
