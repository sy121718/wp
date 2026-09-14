// product_rating.go — 商品评分的写入与查询（issue #30）。
//
// 评分独立成表之后，维护入口与商品更新分离：商品更新只动商品自己的列，
// 评分在这里增删。将来评论域落地时替换的只是「谁调用 AddRating」，接口形状不变。
package productservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// maxRatingScore 评分上限（数据库 CHECK 也是 0~5，这里提前给出明确文案）。
const maxRatingScore = 5

// AddRating 写入一条商品评分。
//
// 归属校验：商品必须存在，project_id 从商品带出（调用方不能自己指定工程 ——
// 否则可以往别的工程的商品上写评分）。
func (s *Service) AddRating(ctx context.Context, req *productdto.AddRatingReq) (res *productdto.RatingResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	if req.Score < 0 || req.Score > maxRatingScore {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	product, err := s.m.Get(ctx, strings.TrimSpace(req.ProductID), "")
	if err != nil {
		return nil, mapNotFound(err)
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}
	row := &productmodel.ProductRatingEntity{
		ID:        uuid.NewString(),
		ProjectID: product.ProjectID,
		ProductID: product.ID,
		Score:     req.Score,
		Source:    source,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err = s.m.CreateRating(ctx, row); err != nil {
		return nil, err
	}
	return s.ratingRespOf(ctx, product.ID)
}

// ListRatings 取某商品的评分明细与投影值。
func (s *Service) ListRatings(ctx context.Context, req *productdto.ListRatingsReq) (res *productdto.RatingResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	if _, err = s.m.Get(ctx, strings.TrimSpace(req.ProductID), ""); err != nil {
		return nil, mapNotFound(err)
	}
	return s.ratingRespOf(ctx, strings.TrimSpace(req.ProductID))
}

// DeleteRating 删除一条评分（不存在即报找不到，不静默成功）。
func (s *Service) DeleteRating(ctx context.Context, req *productdto.DeleteRatingReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	if _, err = s.m.GetRating(ctx, strings.TrimSpace(req.ID)); err != nil {
		return mapNotFound(err)
	}
	return s.m.DeleteRating(ctx, strings.TrimSpace(req.ID))
}

// ratingRespOf 组装某商品的评分明细与投影值。
func (s *Service) ratingRespOf(ctx context.Context, productID string) (res *productdto.RatingResp, err error) {
	rows, err := s.m.ListRatings(ctx, productID)
	if err != nil {
		return nil, err
	}
	items := make([]productmodel.ProductRatingEntity, 0, len(rows))
	out := make([]productdto.RatingItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, *row)
		out = append(out, productdto.RatingItem{
			ID: row.ID, ProductID: row.ProductID, Score: row.Score, Source: row.Source,
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	res = &productdto.RatingResp{Items: out}
	if avg, count, ok := productmodel.RatingSummary(items); ok {
		res.Rating, res.RatingCount, res.HasRating = avg, count, true
	}
	return res, nil
}
