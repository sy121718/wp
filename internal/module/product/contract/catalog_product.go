package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
)

type CatalogProductQueries interface {
	QueryProducts(context.Context, *productdto.ListReq) (*productdto.ProductListResp, error)
	CatalogBrands(context.Context, string) ([]*productdto.BrandResp, error)
	CatalogTags(context.Context, string) ([]*productdto.TagResp, error)
	CatalogAttributes(context.Context, string) ([]*productdto.AttributeResp, error)
	CatalogWarehouses(context.Context, string) ([]productdto.WarehouseOptionResp, error)
	CatalogWarehouseSKUs(context.Context, string, []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error)
	CatalogEntityTemplates(context.Context, string) (*productdto.EntityTemplateCatalogResp, error)
}
