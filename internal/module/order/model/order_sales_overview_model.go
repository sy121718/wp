package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

var (
	orderSalesFrom = OrderEntity{}.TableName() + " AS o"
	orderSalesJoin = "LEFT JOIN " + OrderItemEntity{}.TableName() + " i ON i.order_id = o.id"
)

const orderSalesOverviewSelect = `COUNT(DISTINCT o.id) AS order_count,
       COUNT(i.id) AS item_rows,
       COALESCE(SUM(i.quantity), 0) AS units,
       COALESCE(SUM(i.line_total), 0) AS sales,
       COUNT(DISTINCT o.user_id) AS customers,
       COUNT(DISTINCT CASE WHEN i.cost_price IS NULL AND i.id IS NOT NULL THEN i.id END) AS cost_incomplete_rows,
       COALESCE(SUM(i.line_total), 0)
         - COALESCE(SUM(CASE WHEN i.cost_price IS NOT NULL THEN i.cost_price * i.quantity ELSE 0 END), 0) AS line_margin`

const orderSalesMonthlyInnerSelect = `(date_trunc('month', o.create_time AT TIME ZONE 'UTC'))::date AS ` + orderSalesMonthColumn + `,
       o.id AS order_id,
       o.user_id AS user_id,
       i.line_total AS line_total,
       ` + orderSalesCustomerKind + ` AS customer_kind`

const orderSalesMonthlyOuterSelect = orderSalesMonthColumn + `,
       COUNT(DISTINCT order_id) AS order_count,
       COALESCE(SUM(line_total), 0) AS sales,
       COUNT(DISTINCT user_id) AS customers,
       COUNT(DISTINCT CASE WHEN customer_kind = 'new' THEN order_id END) AS new_order_count,
       COUNT(DISTINCT CASE WHEN customer_kind = 'returning' THEN order_id END) AS returning_order_count,
       COUNT(DISTINCT CASE WHEN customer_kind = 'guest' THEN order_id END) AS guest_order_count,
       COALESCE(SUM(CASE WHEN customer_kind = 'new' THEN line_total ELSE 0 END), 0) AS new_sales,
       COALESCE(SUM(CASE WHEN customer_kind = 'returning' THEN line_total ELSE 0 END), 0) AS returning_sales,
       COALESCE(SUM(CASE WHEN customer_kind = 'guest' THEN line_total ELSE 0 END), 0) AS guest_sales,
       COUNT(DISTINCT CASE WHEN customer_kind = 'new' THEN user_id END) AS new_customers,
       COUNT(DISTINCT CASE WHEN customer_kind = 'returning' THEN user_id END) AS returning_customers`

const orderSalesCustomerKind = `CASE
           WHEN f.first_at IS NULL THEN 'guest'
           WHEN date_trunc('month', f.first_at AT TIME ZONE 'UTC') = date_trunc('month', o.create_time AT TIME ZONE 'UTC') THEN 'new'
           ELSE 'returning'
       END`

const orderSalesMonthColumn = "month"

type OrderSalesFilter struct {
	Statuses []string
}

func (f OrderSalesFilter) statusesText() string {
	if len(f.Statuses) == 0 {
		return strings.Join(paidStatuses, ",")
	}
	return strings.Join(f.Statuses, ",")
}

type OrderSalesOverviewRow struct {
	OrderCount int64 `gorm:"column:order_count"`

	ItemRows int64 `gorm:"column:item_rows"`

	Units int64 `gorm:"column:units"`

	Sales int64 `gorm:"column:sales"`

	Customers int64 `gorm:"column:customers"`

	LineMargin int64 `gorm:"column:line_margin"`

	CostIncompleteRows int64 `gorm:"column:cost_incomplete_rows"`
}

type OrderSalesMonthlyRow struct {
	Month time.Time `gorm:"column:month"`

	OrderCount int64 `gorm:"column:order_count"`

	Sales int64 `gorm:"column:sales"`

	Customers int64 `gorm:"column:customers"`

	NewOrderCount int64 `gorm:"column:new_order_count"`

	ReturningOrderCount int64 `gorm:"column:returning_order_count"`

	GuestOrderCount int64 `gorm:"column:guest_order_count"`

	NewSales int64 `gorm:"column:new_sales"`

	ReturningSales int64 `gorm:"column:returning_sales"`

	GuestSales int64 `gorm:"column:guest_sales"`

	NewCustomers int64 `gorm:"column:new_customers"`

	ReturningCustomers int64 `gorm:"column:returning_customers"`
}

func salesScope(q *gorm.DB, projectID string, from, to time.Time, f OrderSalesFilter) *gorm.DB {
	return q.
		Where("o.project_id = ?", projectID).
		Where("o.create_time >= ?", from).
		Where("o.create_time < ?", to).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", f.statusesText())
}

func (m *OrderModel) SalesOverviewByRange(ctx context.Context, projectID string, from, to time.Time, f OrderSalesFilter) (row OrderSalesOverviewRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Table(orderSalesFrom).Joins(orderSalesJoin).Select(orderSalesOverviewSelect)
		return salesScope(q, projectID, from, to, f).Scan(&row).Error
	})
	return row, err
}

func (m *OrderModel) SalesMonthlyByRange(ctx context.Context, projectID string, from, to time.Time, f OrderSalesFilter) (rows []OrderSalesMonthlyRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		firsts := tx.Table(OrderEntity{}.TableName()+" AS f0").
			Select("f0.user_id AS user_id, MIN(f0.create_time) AS first_at").
			Where("f0.project_id = ?", projectID).
			Where("f0.user_id IS NOT NULL").
			Group("f0.user_id")

		inner := salesScope(
			tx.Table(orderSalesFrom).
				Joins(orderSalesJoin).
				Joins("LEFT JOIN (?) AS f ON f.user_id = o.user_id", firsts).
				Select(orderSalesMonthlyInnerSelect),
			projectID, from, to, f,
		)

		return tx.Table("(?) AS t", inner).
			Select(orderSalesMonthlyOuterSelect).
			Group(orderSalesMonthColumn).
			Order(orderSalesMonthColumn).
			Scan(&rows).Error
	})
	return rows, err
}

func (m *OrderModel) SalesShippingTotalByRange(ctx context.Context, projectID string, from, to time.Time,
	f OrderSalesFilter) (total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return 0, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		inner := salesScope(
			tx.Table(OrderEntity{}.TableName()+" AS o").
				Select("o.id AS id, o.shipping_total AS shipping_total"),
			projectID, from, to, f,
		)
		var row struct {
			Total int64 `gorm:"column:total"`
		}
		if serr := tx.Table("(?) AS s", inner).
			Select("COALESCE(SUM(s.shipping_total), 0) AS total").Scan(&row).Error; serr != nil {
			return serr
		}
		total = row.Total
		return nil
	})
	return total, err
}
