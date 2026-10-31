package model

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type CouponCountMismatchRow struct {
	CouponID    uint64
	ProjectID   string
	Code        string
	UsedCount   int64
	ActualCount int64
}

func (m *CouponModel) CountCoupons(ctx context.Context, projectID string) (n int64, err error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, id, func(tx *gorm.DB) error {
		return tx.Model(&CouponEntity{}).Where("project_id = ?", id).Count(&n).Error
	})
	return n, err
}

func (m *CouponModel) ListCountMismatches(ctx context.Context, projectID string, limit int) (rows []CouponCountMismatchRow, err error) {
	if limit <= 0 {
		limit = 100
	}

	id := strings.TrimSpace(projectID)
	if id == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, id, func(tx *gorm.DB) error {

		redemptions := tx.Model(&CouponRedemptionEntity{}).
			Select("coupon_id, COUNT(*) AS cnt").
			Group("coupon_id")

		return tx.Table(CouponEntity{}.TableName()+" AS c").
			Select("c.id AS coupon_id, c.project_id, c.code, c.used_count, COALESCE(r.cnt, 0) AS actual_count").
			Joins("LEFT JOIN (?) AS r ON r.coupon_id = c.id", redemptions).
			Where("c.used_count <> COALESCE(r.cnt, 0)").
			Where("c.project_id = ?", id).
			Order("c.id").
			Limit(limit).
			Find(&rows).Error
	})
	return rows, err
}
