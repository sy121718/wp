package productservice

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

func (s *Service) CatalogBrands(ctx context.Context, projectID string) ([]*productdto.BrandResp, error) {
	if projectID == "" {
		return []*productdto.BrandResp{}, nil
	}
	return s.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: projectID})
}

func (s *Service) CatalogTags(ctx context.Context, projectID string) ([]*productdto.TagResp, error) {
	if projectID == "" {
		return []*productdto.TagResp{}, nil
	}
	return s.ListTags(ctx, &productdto.ListTagReq{ProjectID: projectID})
}

func (s *Service) CatalogAttributes(ctx context.Context, projectID string) ([]*productdto.AttributeResp, error) {
	if projectID == "" {
		return []*productdto.AttributeResp{}, nil
	}
	return s.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: projectID, Size: 200})
}
