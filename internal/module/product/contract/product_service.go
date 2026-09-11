// Package productcontract product 模块对外契约（issue #5）。
package productcontract

import (
	"context"

	"go_wp/internal/builder/core"
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

	// 属性组与属性值（issue #7）：属性组可跨商品复用，商品只存引用。
	// 变体的笛卡尔积生成不在这里（#8）—— 本契约只保证属性数据可定义、可管理、可被引用。
	CreateAttribute(ctx context.Context, req *productdto.CreateAttributeReq) (res *productdto.AttributeResp, err error)
	UpdateAttribute(ctx context.Context, req *productdto.UpdateAttributeReq) (res *productdto.AttributeResp, err error)
	SetAttributeValues(ctx context.Context, req *productdto.SetAttributeValuesReq) (res *productdto.AttributeResp, err error)
	GetAttribute(ctx context.Context, req *productdto.GetAttributeReq) (res *productdto.AttributeResp, err error)
	ListAttributes(ctx context.Context, req *productdto.ListAttributeReq) (list []*productdto.AttributeResp, err error)
	DeleteAttribute(ctx context.Context, req *productdto.DeleteAttributeReq) (err error)

	// RegisterEntityTypes 把本模块的实体类型（product）注册进实体类型注册表
	// （装配期调用）。注册后内容模板与发布实例即可把商品作为数据源校验字段绑定，
	// 构建期经注册表取商品字段解析器（不反向依赖本模块实现）。
	RegisterEntityTypes(reg core.EntitySourceRegistry) error
}
