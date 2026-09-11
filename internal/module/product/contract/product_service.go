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
	// GenerateVariants 按勾选的属性值生成全部变体组合（issue #8）：笛卡尔积 +
	// 维度/数量上限保护 + 新变体逐字段继承商品级默认值；已存在的组合跳过
	// （重复勾选不产生重复变体）；不传勾选即「全部参与变体的属性组 × 全部启用值」，
	// 无表单路径（批量生成 / 导入 / 接口）与表单路径共用同一份填充规则。
	GenerateVariants(ctx context.Context, req *productdto.GenerateVariantsReq) (res *productdto.GenerateVariantsResp, err error)

	// 属性组与属性值（issue #7）：属性组可跨商品复用，商品只存引用。
	// 变体的笛卡尔积生成在 GenerateVariants（#8）；本组接口只保证属性数据
	// 可定义、可管理、可被引用。
	CreateAttribute(ctx context.Context, req *productdto.CreateAttributeReq) (res *productdto.AttributeResp, err error)
	UpdateAttribute(ctx context.Context, req *productdto.UpdateAttributeReq) (res *productdto.AttributeResp, err error)
	SetAttributeValues(ctx context.Context, req *productdto.SetAttributeValuesReq) (res *productdto.AttributeResp, err error)
	GetAttribute(ctx context.Context, req *productdto.GetAttributeReq) (res *productdto.AttributeResp, err error)
	ListAttributes(ctx context.Context, req *productdto.ListAttributeReq) (list []*productdto.AttributeResp, err error)
	DeleteAttribute(ctx context.Context, req *productdto.DeleteAttributeReq) (err error)

	// 分类与品牌（issue #10）：分类是树形自引用实体（父子层级 + 排序 + slug + SEO），
	// 品牌是独立实体（logo + 描述 + slug + SEO）；商品挂多个分类（附属）并指定主分类，
	// 可指定品牌。删除被引用或被下级依赖的分类/品牌一律拒绝。
	CreateCategory(ctx context.Context, req *productdto.CreateCategoryReq) (res *productdto.CategoryResp, err error)
	UpdateCategory(ctx context.Context, req *productdto.UpdateCategoryReq) (res *productdto.CategoryResp, err error)
	GetCategory(ctx context.Context, req *productdto.GetCategoryReq) (res *productdto.CategoryResp, err error)
	// ListCategories 返回分类树（顶级在数组里，子级挂在 Children，Depth 已填好）。
	ListCategories(ctx context.Context, req *productdto.ListCategoryReq) (list []*productdto.CategoryResp, err error)
	DeleteCategory(ctx context.Context, req *productdto.DeleteCategoryReq) (err error)

	CreateBrand(ctx context.Context, req *productdto.CreateBrandReq) (res *productdto.BrandResp, err error)
	UpdateBrand(ctx context.Context, req *productdto.UpdateBrandReq) (res *productdto.BrandResp, err error)
	GetBrand(ctx context.Context, req *productdto.GetBrandReq) (res *productdto.BrandResp, err error)
	ListBrands(ctx context.Context, req *productdto.ListBrandReq) (list []*productdto.BrandResp, err error)
	DeleteBrand(ctx context.Context, req *productdto.DeleteBrandReq) (err error)

	// RegisterEntityTypes 把本模块的实体类型（product）注册进实体类型注册表
	// （装配期调用）。注册后内容模板与发布实例即可把商品作为数据源校验字段绑定，
	// 构建期经注册表取商品字段解析器（不反向依赖本模块实现）。
	RegisterEntityTypes(reg core.EntitySourceRegistry) error
}
