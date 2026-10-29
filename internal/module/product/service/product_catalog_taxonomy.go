package productservice

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// CatalogBrands 查询已确定工程中的品牌原对象；无工程时不回退或读取数据。
func (s *Service) CatalogBrands(ctx context.Context, projectID string) ([]*productdto.BrandResp, error) {
	if projectID == "" {
		return []*productdto.BrandResp{}, nil
	}
	return s.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: projectID})
}

// CatalogTags 查询已确定工程中的标签原对象；无工程时不触发默认工程解析。
func (s *Service) CatalogTags(ctx context.Context, projectID string) ([]*productdto.TagResp, error) {
	if projectID == "" {
		return []*productdto.TagResp{}, nil
	}
	return s.ListTags(ctx, &productdto.ListTagReq{ProjectID: projectID})
}

// CatalogAttributes 查询已确定工程中的属性原对象，沿用商品创建及编辑的候选范围。
func (s *Service) CatalogAttributes(ctx context.Context, projectID string) ([]*productdto.AttributeResp, error) {
	if projectID == "" {
		return []*productdto.AttributeResp{}, nil
	}
	return s.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: projectID, Size: 200})
}
