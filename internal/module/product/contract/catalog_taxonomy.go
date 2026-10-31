package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

type CatalogTaxonomyQueries interface {
	QueryAttributes(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.AttributeResp], error)
	QueryBrands(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.BrandResp], error)
	QueryTags(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.TagResp], error)
	QueryCategories(context.Context, *productdto.CatalogTaxonomyReq) (*productdto.CategorySearchResp, error)
	CategoryNodes(context.Context, string) ([]*productdto.CategoryResp, error)
	SearchCategoryParents(context.Context, *productdto.CatalogTaxonomyReq) ([]*productdto.CategoryResp, error)
}
