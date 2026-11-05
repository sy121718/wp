package model

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const orderNetTotalSQLExpr = "GREATEST(o.total - COALESCE((SELECT SUM(r.refund_amount) " +
	"FROM order_returns r WHERE r.order_id = o.id AND r.project_id = o.project_id " +
	"AND r.status = ANY(string_to_array(?, ',')::text[])), 0), 0)"

const spentTotalsSelect = "o.user_id, COALESCE(SUM(" + orderNetTotalSQLExpr + "), 0) AS amount"

func (m *OrderModel) SpentTotalsByProject(ctx context.Context, projectID string) (totals map[uint64]int64, err error) {
	var rows []struct {
		UserID uint64 `gorm:"column:user_id"`
		Amount int64  `gorm:"column:amount"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Table(OrderEntity{}.TableName() + " AS o")

		return selectExpr(q, spentTotalsSelect, strings.Join(ReturnedStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.user_id IS NOT NULL").
			Where("o.status = ANY(string_to_array(?, ',')::text[])", strings.Join(DefaultSalesStatuses, ",")).
			Group("o.user_id").
			Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	totals = make(map[uint64]int64, len(rows))
	for _, r := range rows {
		if r.UserID == 0 {

			continue
		}
		totals[r.UserID] = r.Amount
	}
	return totals, nil
}
