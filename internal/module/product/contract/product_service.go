package productcontract

import (
	"context"

	"go_wp/internal/builder/source"
	productdto "go_wp/internal/module/product/dto"
)

type ProductService interface {
	PublicVariantReader
	PublicCatalogReader
	PublicProductOptionsReader
	CatalogProductQueries
	CatalogTaxonomyQueries
	QueryDetailTemplatePanel(context.Context, string, string) (DetailTemplatePanelFacts, error)
	CatalogScopeReader

	ProductDataSource

	VariantSnapshotPort

	Create(ctx context.Context, req *productdto.CreateReq) (res *productdto.ProductResp, err error)
	Update(ctx context.Context, req *productdto.UpdateReq) (res *productdto.ProductResp, err error)
	Get(ctx context.Context, req *productdto.GetReq) (res *productdto.ProductResp, err error)
	List(ctx context.Context, req *productdto.ListReq) (list []*productdto.ProductResp, err error)

	CountProducts(ctx context.Context, req *productdto.ListReq) (n int64, err error)
	Delete(ctx context.Context, req *productdto.DeleteReq) (err error)

	BulkDeleteProducts(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error)
	BulkDeleteProductCategories(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error)
	BulkDeleteProductBrands(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error)
	BulkDeleteProductAttributes(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error)
	BulkDeleteProductTags(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error)

	BulkApplyPricing(ctx context.Context, req *productdto.BulkPricingReq) (res *productdto.BulkPricingResultResp, err error)

	AddRating(ctx context.Context, req *productdto.AddRatingReq) (res *productdto.RatingResp, err error)
	ListRatings(ctx context.Context, req *productdto.ListRatingsReq) (res *productdto.RatingResp, err error)
	DeleteRating(ctx context.Context, req *productdto.DeleteRatingReq) (err error)

	CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (res *productdto.VariantResp, err error)
	UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (res *productdto.VariantResp, err error)
	DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) (err error)

	GenerateVariants(ctx context.Context, req *productdto.GenerateVariantsReq) (res *productdto.GenerateVariantsResp, err error)

	PreviewVariantCombinations(ctx context.Context, req *productdto.PreviewVariantReq) (res *productdto.PreviewVariantResp, err error)
	SaveVariantList(ctx context.Context, req *productdto.SaveVariantListReq) (res *productdto.SaveVariantListResp, err error)

	CreateAttribute(ctx context.Context, req *productdto.CreateAttributeReq) (res *productdto.AttributeResp, err error)
	UpdateAttribute(ctx context.Context, req *productdto.UpdateAttributeReq) (res *productdto.AttributeResp, err error)
	SetAttributeValues(ctx context.Context, req *productdto.SetAttributeValuesReq) (res *productdto.AttributeResp, err error)
	GetAttribute(ctx context.Context, req *productdto.GetAttributeReq) (res *productdto.AttributeResp, err error)
	ListAttributes(ctx context.Context, req *productdto.ListAttributeReq) (list []*productdto.AttributeResp, err error)

	CountAttributes(ctx context.Context, req *productdto.ListAttributeReq) (n int64, err error)
	DeleteAttribute(ctx context.Context, req *productdto.DeleteAttributeReq) (err error)

	CreateCategory(ctx context.Context, req *productdto.CreateCategoryReq) (res *productdto.CategoryResp, err error)
	UpdateCategory(ctx context.Context, req *productdto.UpdateCategoryReq) (res *productdto.CategoryResp, err error)
	GetCategory(ctx context.Context, req *productdto.GetCategoryReq) (res *productdto.CategoryResp, err error)

	ListCategories(ctx context.Context, req *productdto.ListCategoryReq) (list []*productdto.CategoryResp, err error)

	CountCategories(ctx context.Context, req *productdto.ListCategoryReq) (n int64, err error)

	ListCategoryPage(ctx context.Context, req *productdto.ListCategoryPageReq) (res *productdto.CategoryListResp, err error)
	DeleteCategory(ctx context.Context, req *productdto.DeleteCategoryReq) (err error)

	CreateBrand(ctx context.Context, req *productdto.CreateBrandReq) (res *productdto.BrandResp, err error)
	UpdateBrand(ctx context.Context, req *productdto.UpdateBrandReq) (res *productdto.BrandResp, err error)
	GetBrand(ctx context.Context, req *productdto.GetBrandReq) (res *productdto.BrandResp, err error)
	ListBrands(ctx context.Context, req *productdto.ListBrandReq) (list []*productdto.BrandResp, err error)

	CountBrands(ctx context.Context, req *productdto.ListBrandReq) (n int64, err error)
	DeleteBrand(ctx context.Context, req *productdto.DeleteBrandReq) (err error)

	CreateTag(ctx context.Context, req *productdto.CreateTagReq) (res *productdto.TagResp, err error)
	UpdateTag(ctx context.Context, req *productdto.UpdateTagReq) (res *productdto.TagResp, err error)
	GetTag(ctx context.Context, req *productdto.GetTagReq) (res *productdto.TagResp, err error)
	ListTags(ctx context.Context, req *productdto.ListTagReq) (list []*productdto.TagResp, err error)

	CountTags(ctx context.Context, req *productdto.ListTagReq) (n int64, err error)

	ListTagProducts(ctx context.Context, req *productdto.ListTagProductsReq) (*productdto.TagProductsListResp, error)
	DeleteTag(ctx context.Context, req *productdto.DeleteTagReq) (err error)

	ListTagRuleTypes(ctx context.Context) (list []*productdto.TagRuleTypeResp)

	RecalcTags(ctx context.Context, req *productdto.RecalcTagsReq) (res *productdto.RecalcTagsResp, err error)

	PreviewPricing(ctx context.Context, req *productdto.PricingPreviewReq) (res *productdto.PricingPreviewResp, err error)
	ApplyPricing(ctx context.Context, req *productdto.PricingApplyReq) (res *productdto.PricingApplyResp, err error)

	ListPricingRuleTypes(ctx context.Context) (list []*productdto.PricingRuleTypeResp)
	ListPricingRoundingOptions(ctx context.Context) (list []*productdto.PricingRoundingOptionResp)

	ListPriceAdjustments(ctx context.Context, req *productdto.ListPriceAdjustmentReq) (list []*productdto.PriceAdjustmentResp, err error)
	GetPriceAdjustment(ctx context.Context, req *productdto.GetPriceAdjustmentReq) (res *productdto.PriceAdjustmentResp, err error)

	RegisterEntityTypes(reg source.EntitySourceRegistry) error

	ProductTranslationCandidates(ctx context.Context, projectID, productID string) (list []TranslationCandidate, err error)

	ProjectTranslationCandidates(ctx context.Context, projectID string) (list []TranslationCandidate, err error)

	ListVariantOptions(ctx context.Context, req *productdto.ListVariantOptionReq) (list []*productdto.VariantOptionResp, err error)
}
