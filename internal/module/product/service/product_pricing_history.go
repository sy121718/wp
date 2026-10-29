package productservice

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

// pricingHistoryItems 批量读取调价明细；读失败明确返回错误，不能伪装成没有记录。
func (s *Service) pricingHistoryItems(ctx context.Context, projectID string, adjustmentIDs []string) (map[string][]*productdto.PriceAdjustmentItemResp, error) {
	grouped := map[string][]*productdto.PriceAdjustmentItemResp{}
	if len(adjustmentIDs) == 0 {
		return grouped, nil
	}
	details, err := s.m.ListAdjustmentItemsByAdjustmentIDs(ctx, adjustmentIDs, projectID)
	if err != nil {
		return nil, err
	}
	// 每批的展示上限与单批次详情一致（maxPricingAdjustmentItems）：截断在取商品名之前，
	// 与单批次版本「先按上限取、再补名字」的顺序相同。批量 SQL 里做不了这件事 ——
	// 跨批次的全局 LIMIT 不是「每批 N 条」，而是按批次顺序整体切一刀。
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
