package productservice

import (
	"context"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// PublicVariants 按工程读取已发布商品的实时事实，不经过构建器，不读取成本。
func (s *Service) PublicVariants(ctx context.Context, req *productdto.PublicVariantsReq) ([]productdto.PublicVariantResp, error) {
	ids, err := req.IDs()
	if err != nil {
		return nil, err
	}
	variants, err := s.m.ListVariantsByIDs(ctx, ids, req.ProjectID)
	if err != nil {
		return nil, err
	}
	productIDs := make([]string, 0, len(variants))
	for _, v := range variants {
		productIDs = append(productIDs, v.ProductID)
	}
	products, err := s.m.ListByIDs(ctx, dedupeNonEmpty(productIDs), req.ProjectID)
	if err != nil {
		return nil, err
	}
	published := map[string]bool{}
	for _, p := range products {
		published[p.ID] = strings.EqualFold(p.ProjectID, req.ProjectID) && p.Status == productenums.StatusPublished
	}
	byID := map[string]productdto.PublicVariantResp{}
	publicIDs := make([]string, 0, len(variants))
	for _, v := range variants {
		if !published[v.ProductID] {
			continue
		}
		price := yuanToCents(v.Price)
		byID[v.ID] = productdto.PublicVariantResp{ID: v.ID, Known: true, Enabled: v.Enabled, Price: &price}
		publicIDs = append(publicIDs, v.ID)
	}
	available, err := s.VariantAvailabilities(ctx, req.ProjectID, publicIDs)
	if err != nil {
		return nil, err
	}
	result := make([]productdto.PublicVariantResp, 0, len(ids))
	for _, id := range ids {
		fact, ok := byID[id]
		if !ok {
			fact = productdto.PublicVariantResp{ID: id}
		}
		if n, known := available[id]; known && fact.Known {
			fact.Available = &n
		}
		result = append(result, fact)
	}
	return result, nil
}
