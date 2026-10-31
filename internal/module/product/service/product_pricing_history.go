package productservice

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

func (s *Service) pricingHistoryItems(ctx context.Context, projectID string, adjustmentIDs []string) (map[string][]*productdto.PriceAdjustmentItemResp, error) {
	grouped := map[string][]*productdto.PriceAdjustmentItemResp{}
	if len(adjustmentIDs) == 0 {
		return grouped, nil
	}
	details, err := s.m.ListAdjustmentItemsByAdjustmentIDs(ctx, adjustmentIDs, projectID)
	if err != nil {
		return nil, err
	}

	perAdjustment := make(map[string][]*productmodel.PriceAdjustmentItemEntity, len(adjustmentIDs))
	kept := make([]*productmodel.PriceAdjustmentItemEntity, 0, len(details))
	for _, d := range details {
		if d == nil {
			continue
		}
		group := perAdjustment[d.AdjustmentID]
		if len(group) >= maxPricingAdjustmentItems {
			continue
		}
		perAdjustment[d.AdjustmentID] = append(group, d)
		kept = append(kept, d)
	}
	names, nerr := s.adjustmentProductNames(ctx, projectID, kept)
	if nerr != nil {
		return nil, nerr
	}
	for adjID, group := range perAdjustment {
		grouped[adjID] = adjustmentItemResps(group, names)
	}
	return grouped, nil
}
