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

	// 标签（issue #11）：手工标签与自动标签同表（kind 区分）。
	//   · 手工标签可建、可手工挂到商品（引用校验同分类/品牌：同工程 + 必须存在）；
	//   · 自动标签只接受内置规则类型与白名单参数，不接受自由表达式，非法规则被拒绝；
	//   · 自动标签的归属由明确定义的重算时机维护（商品/变体写操作后、标签定义变更后、
	//     显式调用），重算只替换自己那一个 tag id，绝不覆盖手工标签；
	//   · GetTag / ListTagProducts 提供「某标签命中哪些商品」（后台核对用）。
	CreateTag(ctx context.Context, req *productdto.CreateTagReq) (res *productdto.TagResp, err error)
	UpdateTag(ctx context.Context, req *productdto.UpdateTagReq) (res *productdto.TagResp, err error)
	GetTag(ctx context.Context, req *productdto.GetTagReq) (res *productdto.TagResp, err error)
	ListTags(ctx context.Context, req *productdto.ListTagReq) (list []*productdto.TagResp, err error)
	// ListTagProducts 某标签命中的商品（limit <= 0 用服务端默认上限）。
	ListTagProducts(ctx context.Context, req *productdto.ListTagProductsReq) (list []*productdto.TagProductResp, err error)
	DeleteTag(ctx context.Context, req *productdto.DeleteTagReq) (err error)
	// ListTagRuleTypes 内置规则类型清单（后台规则下拉与参数说明的唯一来源）。
	ListTagRuleTypes(ctx context.Context) (list []*productdto.TagRuleTypeResp)
	// RecalcTags 手动触发重算：TagID 为空表示重算该工程下全部自动标签。
	RecalcTags(ctx context.Context, req *productdto.RecalcTagsReq) (res *productdto.RecalcTagsResp, err error)

	// 定价工具（issue #13）：四种内置规则（成本乘倍数 / 成本加价 / 目标毛利率 / 统一售价）
	// + 尾数处理，可对单个 SKU / 单商品全部变体 / 筛选集批量应用。
	//   · PreviewPricing 试算：不落库、不留痕，与 ApplyPricing 共用同一份算价逻辑；
	//   · ApplyPricing 应用：售价写回 product_variants.price（不是运行时计算）+ 写留痕台账；
	//     只有真正变化的变体才写库与留痕；一条都没变时返回 ErrPricingNothingChanged；
	//     落库后立刻按「变体写操作后」重算本工程自动标签（#11 的重算时机）。
	// 两个方法都不进构建管线：构建期读的是落库后的确定值。
	PreviewPricing(ctx context.Context, req *productdto.PricingPreviewReq) (res *productdto.PricingPreviewResp, err error)
	ApplyPricing(ctx context.Context, req *productdto.PricingApplyReq) (res *productdto.PricingApplyResp, err error)
	// ListPricingRuleTypes / ListPricingRoundingOptions 内置规则与尾数清单
	// （后台下拉与参数说明的唯一来源）。
	ListPricingRuleTypes(ctx context.Context) (list []*productdto.PricingRuleTypeResp)
	ListPricingRoundingOptions(ctx context.Context) (list []*productdto.PricingRoundingOptionResp)
	// ListPriceAdjustments / GetPriceAdjustment 调价留痕（验收 4：改动有留痕）。
	ListPriceAdjustments(ctx context.Context, req *productdto.ListPriceAdjustmentReq) (list []*productdto.PriceAdjustmentResp, err error)
	GetPriceAdjustment(ctx context.Context, req *productdto.GetPriceAdjustmentReq) (res *productdto.PriceAdjustmentResp, err error)

	// RegisterEntityTypes 把本模块的实体类型（product）注册进实体类型注册表
	// （装配期调用）。注册后内容模板与发布实例即可把商品作为数据源校验字段绑定，
	// 构建期经注册表取商品字段解析器（不反向依赖本模块实现）。
	RegisterEntityTypes(reg core.EntitySourceRegistry) error

	// ProductTranslationCandidates 单个商品及其引用实体的全部可翻译文本（issue #12）。
	// 翻译工作台在 dashboard 模块，跨模块只能经契约取值，故这两个方法留在契约上。
	ProductTranslationCandidates(ctx context.Context, productID string) (list []TranslationCandidate, err error)
	// ProjectTranslationCandidates 工程内全部商品域可翻译文本（按 (hash, context) 去重）。
	ProjectTranslationCandidates(ctx context.Context, projectID string) (list []TranslationCandidate, err error)
	// —— 捆绑品（issue #20）——
	// 选项规则落在 products.bundle_items（迁移 114 起是带规则的对象，不再是裸数组）：
	// 选项来自**跨商品挑选的已存在 SKU**，每项带必选 / 可选与默认 / 最小 / 最大数量；
	// 主体可配选项数量上限与整单最小 / 最大总件数。
	//   · SetBundleConfig 整体替换配置，非法 / 自相矛盾的配置在保存时被拒并给出可读原因；
	//   · GetBundleConfig 读配置 + 每个选项的 SKU 快照与可用量（可用量只读 inventory 真源）；
	//   · ValidateBundleSelection 整单硬校验 + 算价（纯读）：前端反馈与后端拦截共用它；
	//   · ListBundleSKUs 后台配置器的 SKU 数据源（跨商品）。
	GetBundleConfig(ctx context.Context, req *productdto.GetBundleConfigReq) (res *productdto.BundleConfigResp, err error)
	SetBundleConfig(ctx context.Context, req *productdto.SetBundleConfigReq) (res *productdto.BundleConfigResp, err error)
	ValidateBundleSelection(ctx context.Context, req *productdto.ValidateBundleSelectionReq) (res *productdto.BundleSelectionResp, err error)
	ListBundleSKUs(ctx context.Context, req *productdto.ListBundleSKUReq) (list []*productdto.BundleSKUResp, err error)
}
