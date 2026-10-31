package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func orderCohortScope(tx *gorm.DB, projectID string, from, to time.Time) (cohort, sizes *gorm.DB) {
	statuses := strings.Join(paidStatuses, ",")
	firsts := tx.Table(OrderEntity{}.TableName()+" AS o").
		Select("o.user_id, MIN(o.create_time) AS first_at").
		Where("o.project_id = ?", projectID).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", statuses).
		Where("o.user_id IS NOT NULL").
		Group("o.user_id")
	cohort = tx.Table("(?) AS f", firsts).
		Select("f.user_id, (date_trunc('month', f.first_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS cohort_month").
		Where("f.first_at >= ?", from).
		Where("f.first_at < ?", to)
	sizes = tx.Table("(?) AS c", cohort).
		Select("c.cohort_month, COUNT(*) AS cohort_size").
		Group("c.cohort_month")
	return cohort, sizes
}

const orderCohortActivitySelect = "a.cohort_month, a.activity_month, " +
	"COUNT(DISTINCT a.user_id) AS active_customers, s.cohort_size"

type OrderCohortRetentionRow struct {
	CohortMonth time.Time `gorm:"column:cohort_month"`

	ActivityMonth time.Time `gorm:"column:activity_month"`

	ActiveCustomers int64 `gorm:"column:active_customers"`

	CohortSize int64 `gorm:"column:cohort_size"`
}

func (m *OrderModel) CohortRetention(ctx context.Context, projectID string, from, to time.Time) (rows []OrderCohortRetentionRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	statuses := strings.Join(paidStatuses, ",")
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		cohort, sizes := orderCohortScope(tx, projectID, from, to)
		active := tx.Table("(?) AS c", cohort).
			Select("c.cohort_month, (date_trunc('month', o.create_time AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS activity_month, o.user_id").
			Joins("JOIN "+OrderEntity{}.TableName()+" AS o ON o.user_id = c.user_id").
			Where("o.project_id = ?", projectID).
			Where("o.status = ANY(string_to_array(?, ',')::text[])", statuses)

		return tx.Table("(?) AS a", active).
			Joins("JOIN (?) AS s ON s.cohort_month = a.cohort_month", sizes).
			Select(orderCohortActivitySelect).
			Group("a.cohort_month, a.activity_month, s.cohort_size").
			Order("a.cohort_month, a.activity_month").
			Scan(&rows).Error
	})
	return rows, err
}
