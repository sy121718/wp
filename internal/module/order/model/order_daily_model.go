package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const orderBucketSelect = "COUNT(*) AS order_count, " +
	"COUNT(*) FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])) AS paid_order_count, " +
	"COALESCE(SUM(" + orderNetTotalSQLExpr + ") FILTER (WHERE o.status = ANY(string_to_array(?, ',')::text[])), 0) AS net_sales"

const orderDailyBucketSelect = "date_trunc('day', o.create_time AT TIME ZONE 'UTC')::date AS day, " + orderBucketSelect

type OrderDailyPoint struct {
	Day time.Time `gorm:"column:day"`

	OrderCount int64 `gorm:"column:order_count"`

	PaidOrderCount int64 `gorm:"column:paid_order_count"`

	NetSales int64 `gorm:"column:net_sales"`
}

func (m *OrderModel) DailyByRange(ctx context.Context, projectID string, from, to time.Time) (rows []OrderDailyPoint, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(orderDailyBucketSelect,
				strings.Join(paidStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(paidStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.create_time >= ?", from).
			Where("o.create_time < ?", to).
			Group("day").
			Order("day").
			Scan(&rows).Error
	})
	return rows, err
}

const orderHourlyBucketSelect = "(date_trunc('hour', o.create_time AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS day, " + orderBucketSelect

func (m *OrderModel) HourlyByRange(ctx context.Context, projectID string, from, to time.Time) (rows []OrderDailyPoint, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(OrderEntity{}.TableName()+" AS o").
			Select(orderHourlyBucketSelect,
				strings.Join(paidStatuses, ","),
				strings.Join(ReturnedStatuses, ","),
				strings.Join(paidStatuses, ",")).
			Where("o.project_id = ?", projectID).
			Where("o.create_time >= ?", from).
			Where("o.create_time < ?", to).
			Group("day").
			Order("day").
			Scan(&rows).Error
	})
	return rows, err
}
