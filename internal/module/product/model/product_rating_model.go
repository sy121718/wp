package productmodel

// product_rating_model.go — 商品评分明细（issue #30）。
//
// 评分是**独立表 + 明细行**，不是商品上的两个列：
//
//	· 一行 = 一条评分（score 0~5）；
//	· 商品查询经 hasMany 关联 Preload 明细（一次批量拉回，不是 N+1）；
//	· 平均分与条数由明细**算出来**（投影），不落库。
//
// 为什么存明细而不是存聚合：聚合值（评分 4.5、120 条）是明细的函数。
// 存成商品列意味着每加一条评分都要回写商品行、并且丢掉明细 ——
// 将来评论域落地时无处可接，而明细表只需要换个写入方。

import (
	"context"
	"time"
)

// ProductRatingEntity 一条商品评分。
type ProductRatingEntity struct {
	ID        string    `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string    `gorm:"column:project_id;type:uuid;not null"`
	ProductID string    `gorm:"column:product_id;type:uuid;not null"`
	Score     float64   `gorm:"column:score;type:numeric(3,2);not null"`
	Source    string    `gorm:"column:source;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

// TableName 实现 gorm 表名。
func (ProductRatingEntity) TableName() string { return "product_ratings" }

// RatingSummary 由评分明细算出的投影值（issue #30）。
//
// ok=false 表示**一条评分都没有** —— 与「平均分 0」严格区分：
// 组件据此把该商品排在评分排序的最后、并让它不入选最低评分筛选。
func RatingSummary(ratings []ProductRatingEntity) (avg float64, count int, ok bool) {
	if len(ratings) == 0 {
		return 0, 0, false
	}
	total := 0.0
	for _, r := range ratings {
		total += r.Score
	}
	return total / float64(len(ratings)), len(ratings), true
}

// RatingSummaryOf 商品自身的评分投影（明细已 Preload 时直接算）。
func (p *ProductEntity) RatingSummaryOf() (avg float64, count int, ok bool) {
	if p == nil {
		return 0, 0, false
	}
	if p.RatingCount != nil && *p.RatingCount > 0 && p.RatingAvg != nil {
		return *p.RatingAvg, *p.RatingCount, true
	}
	return RatingSummary(p.Ratings)
}

// ListRatings 取某商品的评分明细（按时间倒序：最近的在前）。
func (m *Model) ListRatings(ctx context.Context, productID string) (list []*ProductRatingEntity, err error) {
	err = m.RatingDB(ctx).Where("product_id = ?", productID).Order("created_at DESC, id DESC").Find(&list).Error
	return list, err
}

// CreateRating 写入一条评分（projectID 由调用方从商品带出，保持与商品同工程）。
func (m *Model) CreateRating(ctx context.Context, e *ProductRatingEntity) (err error) {
	return m.RatingDB(ctx).Create(e).Error
}

// GetRating 按 id 取一条评分（校验归属用）。
func (m *Model) GetRating(ctx context.Context, id string) (e *ProductRatingEntity, err error) {
	var row ProductRatingEntity
	if err = m.RatingDB(ctx).Where("id = ?", id).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteRating 删除一条评分。
func (m *Model) DeleteRating(ctx context.Context, id string) (err error) {
	return m.RatingDB(ctx).Where("id = ?", id).Delete(&ProductRatingEntity{}).Error
}
