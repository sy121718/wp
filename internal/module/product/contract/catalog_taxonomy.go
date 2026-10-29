package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

// CatalogTaxonomyQueries 提供目录查询事实，页面与 API 可复用同一数据对象。
type CatalogTaxonomyQueries interface {
	QueryAttributes(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.AttributeResp], error)
	QueryBrands(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.BrandResp], error)
	QueryTags(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.TagResp], error)
	QueryCategories(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.CategorySearchResp, error)
	CategoryNodes(context.Context, string) ([]*productdto.CategoryResp, error)
	SearchCategoryParents(context.Context, *productdto.CatalogTaxonomyReq) ([]*productdto.CategoryResp, error)
}
