package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const orderRangeSummarySelect = "COUNT(*) AS order_count, " +
	"COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) AS paid_order_count, " +
	"COALESCE(SUM(" + orderNetTotalSQLExpr + ") FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])), 0) AS net_sales"

type OrderRangeSummaryRow struct {
	OrderCount     int64 `gorm:"column:order_count"`
	PaidOrderCount int64 `gorm:"column:paid_order_count"`

	NetSales int64 `gorm:"column:net_sales"`
}

var ErrRangeRequired = errors.New("order: 需要显式的区间参数")

func (m *OrderModel) SummaryByRange(ctx context.Context, projectID string, from, to time.Time) (row OrderRangeSummaryRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(orderRangeSummarySelect,

				strings.Join(DefaultSalesStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(DefaultSalesStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.create_time >= ?", from).
			Where("o.create_time < ?", to).
			Scan(&row).Error
	})
	return row, err
}
