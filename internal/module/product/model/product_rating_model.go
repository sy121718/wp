package productmodel

import (
	"context"
	"strings"
	"time"

	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProductRatingEntity struct {
	ID        string    `gorm:"column:id;primaryKey"`
	ProjectID string    `gorm:"column:project_id;not null"`
	ProductID string    `gorm:"column:product_id;not null"`
	Score     float64   `gorm:"column:score;not null"`
	Source    string    `gorm:"column:source;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

func (ProductRatingEntity) TableName() string { return "product_ratings" }

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

func (p *ProductEntity) RatingSummaryOf() (avg float64, count int, ok bool) {
	if p == nil {
		return 0, 0, false
	}
	if p.RatingCount != nil && *p.RatingCount > 0 && p.RatingAvg != nil {
		return *p.RatingAvg, *p.RatingCount, true
	}
	return RatingSummary(p.Ratings)
}

func (m *Model) ListRatings(ctx context.Context, productID, projectID string) (list []*ProductRatingEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		list, err = m.ListRatingsTx(ctx, tx, productID, projectID)
		return err
	})
	return list, err
}

type RatingAgg struct {
	Avg float64

	Count int
}

func (m *Model) RatingSummariesByProducts(ctx context.Context, projectID string, productIDs []string) (map[string]RatingAgg, error) {
	ids := make([]string, 0, len(productIDs))
	for _, id := range productIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	out := make(map[string]RatingAgg, len(ids))
	for start := 0; start < len(ids); start += idBatchSize {
		end := min(start+idBatchSize, len(ids))
		var rows []*ProductRatingEntity
		if err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
			return tx.WithContext(ctx).Model(&ProductRatingEntity{}).
				Where("project_id = ? AND product_id IN ?", projectID, ids[start:end]).
				Order("product_id, create_time DESC, id DESC").
				Find(&rows).Error
		}); err != nil {
			return nil, err
		}
		grouped := make(map[string][]ProductRatingEntity, len(rows))
		for _, row := range rows {
			grouped[row.ProductID] = append(grouped[row.ProductID], *row)
		}
		for productID, items := range grouped {
			if avg, count, ok := RatingSummary(items); ok {
				out[productID] = RatingAgg{Avg: avg, Count: count}
			}
		}
	}
	return out, nil
}

func (m *Model) CreateRating(ctx context.Context, e *ProductRatingEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductRatingEntity{}).Create(e).Error
	})
}

func (m *Model) GetRating(ctx context.Context, id, projectID string) (e *ProductRatingEntity, err error) {
	var row ProductRatingEntity
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductRatingEntity{}).Where("id = ? AND project_id = ?", id, projectID).Take(&row).Error
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) DeleteRating(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductRatingEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).Delete(&ProductRatingEntity{}).Error
	})
}

func (m *Model) CreateRatingTx(ctx context.Context, tx *gorm.DB, e *ProductRatingEntity) error {
	return tx.WithContext(ctx).Model(&ProductRatingEntity{}).Create(e).Error
}

// DeleteRatingTx 删除并返回同工程的评分事实；不存在时拒绝，不重复产生事件。
func (m *Model) DeleteRatingTx(ctx context.Context, tx *gorm.DB, id, projectID string) (*ProductRatingEntity, error) {
	var row ProductRatingEntity
	result := tx.WithContext(ctx).Clauses(clause.Returning{}).
		Where("id = ? AND project_id = ?", id, projectID).Delete(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

// ListRatingsTx 在当前事务内读取评分明细，供写入结果与聚合使用同一份事实。
func (m *Model) ListRatingsTx(ctx context.Context, tx *gorm.DB, productID, projectID string) (list []*ProductRatingEntity, err error) {
	err = tx.WithContext(ctx).Model(&ProductRatingEntity{}).
		Where("project_id = ? AND product_id = ?", projectID, productID).
		Order("create_time DESC, id DESC").Find(&list).Error
	return list, err
}
