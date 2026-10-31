package productservice

import (
	"context"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/siteurl"
)

func (s *Service) QueryDetailTemplates(ctx context.Context, projectID string) ([]productcontract.DetailTemplateCandidateView, error) {
	if s.pageDetailTemplates == nil {
		return []productcontract.DetailTemplateCandidateView{}, nil
	}
	rows, err := s.pageDetailTemplates.DetailTemplateCandidates(ctx, projectID, productcontract.EntityTypeProduct)
	if rows == nil {
		rows = []productcontract.DetailTemplateCandidateView{}
	}
	return rows, err
}

func (s *Service) QueryDefaultDetailTemplate(ctx context.Context, projectID string) (string, error) {
	if s.pageDetailTemplates == nil {
		return "", nil
	}
	return s.pageDetailTemplates.DefaultDetailTemplate(ctx, projectID, productcontract.EntityTypeProduct)
}

func (s *Service) QueryBoundDetailTemplate(ctx context.Context, projectID, productID string) (productcontract.BoundTemplateView, error) {
	if s.pageDetailTemplates == nil {
		return productcontract.BoundTemplateView{}, nil
	}
	return s.pageDetailTemplates.BoundDetailTemplate(ctx, projectID, productcontract.EntityTypeProduct, productID)
}

func (s *Service) ProductDetailPath(ctx context.Context, projectID string, product *productdto.ProductResp) string {
	return siteurl.DetailPathForProject(ctx, s.project, projectID, siteurl.KindProduct, product.Slug, product.ID)
}
