package productservice

import (
	"context"
	"fmt"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"strings"
)

// QueryProducts 按同一筛选条件先计数再收敛页码，整页补充挂载关系与评分汇总。
// 查询次数与页内商品数无关；读取失败不降级成无评分或缺失商品。
func (s *Service) QueryProducts(ctx context.Context, req *productdto.ListReq) (*productdto.ProductListResp, error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	query := *req
	query.Keyword, query.Status = strings.TrimSpace(query.Keyword), strings.TrimSpace(query.Status)
	query.Page, query.Size = pageArgs(&query)
	result := &productdto.ProductListResp{Items: []*productdto.ProductResp{}, Page: query.Page, Size: query.Size}
	if query.ProjectID == "" {
		return result, nil
	}
	total, err := s.CountProducts(ctx, &query)
	if err != nil {
		return nil, err
	}
	last := int((total + int64(query.Size) - 1) / int64(query.Size))
	if last < 1 {
		last = 1
	}
	if query.Page > last {
		query.Page = last
	}
	result.Page, result.Total = query.Page, total
	result.Items, err = s.List(ctx, &query)
	if err != nil {
		return nil, err
	}
	if err := s.fillProductListFacts(ctx, query.ProjectID, result.Items); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) fillProductListFacts(ctx context.Context, projectID string, items []*productdto.ProductResp) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil && item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	facts, err := s.m.ListRowFacts(ctx, projectID, ids)
	if err != nil {
		return fmt.Errorf("读取商品挂载关系失败: %w", err)
	}
	ratings, err := s.m.RatingSummariesByProducts(ctx, projectID, ids)
	if err != nil {
		return fmt.Errorf("读取商品评分汇总失败: %w", err)
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		fact, ok := facts[item.ID]
		if !ok {
			return fmt.Errorf("商品 %s 的挂载关系缺失", item.ID)
		}
		applyRowFacts(item, fact)
		item.Rating = ratingRespOfAgg(ratings[item.ID])
	}
	return nil
}

func ratingRespOfAgg(agg productmodel.RatingAgg) *productdto.RatingResp {
	return &productdto.RatingResp{Rating: agg.Avg, RatingCount: agg.Count, HasRating: agg.Count > 0}
}

func applyRowFacts(item *productdto.ProductResp, facts productmodel.ProductRowFacts) {
	if item == nil {
		return
	}
	item.CategoryIDs = decodeStrings(facts.CategoryIDs)
	item.TagIDs = decodeStrings(facts.TagIDs)
	item.PrimaryCategoryID = facts.PrimaryCategoryID
	item.BrandID = facts.BrandID
}
