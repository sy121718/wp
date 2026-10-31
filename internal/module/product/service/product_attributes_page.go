package productservice

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/pagination"
)

func (s *Service) QueryAttributes(ctx context.Context, query *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.AttributeResp], error) {
	q := normalizedTaxonomyQuery(query)
	projectID := q.ProjectID
	result := &productdto.TaxonomyListResp[*productdto.AttributeResp]{Items: []*productdto.AttributeResp{}, Page: 1, Size: q.Size}
	if projectID == "" {
		return result, nil
	}
	req := &productdto.ListAttributeReq{ProjectID: projectID, Keyword: q.Keyword, Variation: q.Variation}
	total, err := s.CountAttributes(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Page, req.Size = pagination.ClampPage(q.Page, q.Size, total), q.Size
	result.Items, err = s.ListAttributes(ctx, req)
	if err != nil {
		return nil, err
	}
	result.Total, result.Page = total, req.Page
	return result, nil
}
