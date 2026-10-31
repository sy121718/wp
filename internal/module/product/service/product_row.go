package productservice

import (
	"context"
	"fmt"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/pagination"
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
	paging := pageArgs(&query)
	query.Page, query.Size = paging.Page, paging.Size
	result := &productdto.ProductListResp{Items: []*productdto.ProductResp{}, Page: query.Page, Size: query.Size}
	if query.ProjectID == "" {
		return result, nil
	}
	total, err := s.CountProducts(ctx, &query)
	if err != nil {
		return nil, err
	}
	query.Page = pagination.ClampPage(query.Page, query.Size, total)
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
	ratings, err := s.m.RatingSummariesByProducts(ctx, projectID, ids)
	if err != nil {
		return fmt.Errorf("读取商品评分汇总失败: %w", err)
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		item.Rating = ratingRespOfAgg(ratings[item.ID])
	}
	return nil
}

func ratingRespOfAgg(agg productmodel.RatingAgg) *productdto.RatingResp {
	return &productdto.RatingResp{Rating: agg.Avg, RatingCount: agg.Count, HasRating: agg.Count > 0}
}
