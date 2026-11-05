package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func orderCustomerScope(tx *gorm.DB, projectID string, from, to time.Time, statuses []string) (ranged, firsts *gorm.DB) {
	// 与销售额走同一份状态串（salesStatusesText）：同屏两个数字必须回答同一批行。
	statusesText := salesStatusesText(statuses)
	orders := OrderEntity{}.TableName() + " AS o"
	ranged = tx.Table(orders).
		Select("o.user_id, COUNT(*) AS order_count").
		Where("o.project_id = ?", projectID).
		Where("o.create_time >= ?", from).
		Where("o.create_time < ?", to).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", statusesText).
		Where("o.user_id IS NOT NULL").
		Group("o.user_id")
	firsts = tx.Table(orders).
		Select("o.user_id, MIN(o.create_time) AS first_at").
		Where("o.project_id = ?", projectID).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", statusesText).
		Where("o.user_id IS NOT NULL").
		Group("o.user_id")
	return ranged, firsts
}

const (
	customerSegmentWhereNew          = "f.first_at >= ?"
	customerSegmentWhereReturning    = "f.first_at < ?"
	customerSegmentWhereRepurchasing = "r.order_count >= 2"
)

var orderCustomerGrowthSelect = "COUNT(*) AS ordering_customers, " +
	"COUNT(*) FILTER (WHERE f.first_at >= ?) AS new_customers, " +
	"COUNT(*) FILTER (WHERE f.first_at < ? AND " + customerRepurchaseWhere(0) + ") AS new_repurchasers, " +
	"COUNT(*) FILTER (WHERE " + customerRepurchaseWhere(0) + ") AS repurchasers, " +
	"COUNT(*) FILTER (WHERE f.first_at < ?) AS returning_customers"

type OrderCustomerGrowthRow struct {
	OrderingCustomers int64 `gorm:"column:ordering_customers"`

	NewCustomers int64 `gorm:"column:new_customers"`

	NewRepurchasers int64 `gorm:"column:new_repurchasers"`

	Repurchasers int64 `gorm:"column:repurchasers"`

	ReturningCustomers int64 `gorm:"column:returning_customers"`
}

func (m *OrderModel) CustomerGrowthByRange(ctx context.Context, projectID string, from, to time.Time, statuses []string) (row OrderCustomerGrowthRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		ranged, firsts := orderCustomerScope(tx, projectID, from, to, statuses)
		q := tx.Table("(?) AS r", ranged).
			Joins("JOIN (?) AS f ON f.user_id = r.user_id", firsts)

		return selectExpr(q, orderCustomerGrowthSelect, from, from, from).
			Scan(&row).Error
	})
	return row, err
}
