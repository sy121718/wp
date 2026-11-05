package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func orderItemScope(q *gorm.DB, projectID string, from, to time.Time) *gorm.DB {
	return q.
		Where("o.project_id = ?", projectID).
		Where("o.create_time >= ?", from).
		Where("o.create_time < ?", to).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", strings.Join(DefaultSalesStatuses, ","))
}

const orderSoldQuantitySelect = "COALESCE(SUM(i.quantity), 0) AS quantity, COUNT(DISTINCT i.order_id) AS order_count"

const orderTopProductsSelect = "i.product_id, i.product_name, i.sku, " +
	"SUM(i.quantity) AS quantity, COALESCE(SUM(i.line_total), 0) AS amount"

const MaxTopProductLimit = 50

type OrderTopProductRow struct {
	ProductID   string `gorm:"column:product_id"`
	ProductName string `gorm:"column:product_name"`
	SKU         string `gorm:"column:sku"`

	Quantity int64 `gorm:"column:quantity"`

	Amount int64 `gorm:"column:amount"`
}

type OrderSoldQuantityRow struct {
	Quantity int64 `gorm:"column:quantity"`

	OrderCount int64 `gorm:"column:order_count"`
}

func (m *OrderModel) SoldQuantityByRange(ctx context.Context, projectID string, from, to time.Time) (row OrderSoldQuantityRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return orderItemScope(orderSalesItemsJoin(tx), projectID, from, to).
			Select(orderSoldQuantitySelect).
			Scan(&row).Error
	})
	return row, nil
}

func orderSalesItemsJoin(tx *gorm.DB) *gorm.DB {
	return tx.Table(OrderItemEntity{}.TableName() + " AS i").
		Joins("JOIN " + OrderEntity{}.TableName() + " AS o ON o.id = i.order_id")
}

func (m *OrderModel) TopProductsByRange(ctx context.Context, projectID string, from, to time.Time, limit int) (rows []OrderTopProductRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	if limit <= 0 || limit > MaxTopProductLimit {
		limit = MaxTopProductLimit
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return orderItemScope(orderSalesItemsJoin(tx), projectID, from, to).
			Select(orderTopProductsSelect).
			Group("i.product_id, i.product_name, i.sku").
			Order("quantity DESC, amount DESC, i.product_id").
			Limit(limit).
			Scan(&rows).Error
	})
	return rows, nil
}
