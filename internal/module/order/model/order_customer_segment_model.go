package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

var ErrSegmentUnknown = errors.New("order: 未知的客户分段")

type CustomerSegment string

const (
	CustomerSegmentNew CustomerSegment = "new"

	CustomerSegmentReturning CustomerSegment = "returning"

	CustomerSegmentRepurchasing CustomerSegment = "repurchasing"
)

const customerRepurchaseMinOrders = 2

func customerRepurchaseWhere(minOrders int) string {
	if minOrders < customerRepurchaseMinOrders {
		minOrders = customerRepurchaseMinOrders
	}
	return fmt.Sprintf("r.order_count >= %d", minOrders)
}

func customerSegmentWhere(segment CustomerSegment, minOrders int) (where string, needsFrom bool, ok bool) {
	switch segment {
	case CustomerSegmentNew:
		return customerSegmentWhereNew, true, true
	case CustomerSegmentReturning:
		return customerSegmentWhereReturning, true, true
	case CustomerSegmentRepurchasing:
		return customerRepurchaseWhere(minOrders), false, true
	}
	return "", false, false
}

const customerSegmentSelect = "r.user_id, COUNT(*) OVER () AS total"

func (m *OrderModel) CustomerSegmentIDsByRange(ctx context.Context, projectID string, from, to time.Time, segment CustomerSegment, minOrders, limit, offset int) (ids []int64, total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, 0, ErrRangeRequired
	}
	where, needsFrom, ok := customerSegmentWhere(segment, minOrders)
	if !ok {
		return nil, 0, ErrSegmentUnknown
	}
	if limit <= 0 {
		return nil, 0, ErrRangeRequired
	}
	type row struct {
		UserID int64 `gorm:"column:user_id"`
		Total  int64 `gorm:"column:total"`
	}
	var rows []row
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		ranged, firsts := orderCustomerScope(tx, projectID, from, to)
		q := tx.Table("(?) AS r", ranged).
			Joins("JOIN (?) AS f ON f.user_id = r.user_id", firsts).
			Select(customerSegmentSelect)

		if needsFrom {
			q = q.Where(where, from)
		} else {
			q = q.Where(where)
		}

		return q.Order("r.user_id").Limit(limit).Offset(offset).Scan(&rows).Error
	})
	if err != nil {
		return nil, 0, err
	}
	ids = make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.UserID)
		total = r.Total
	}
	return ids, total, nil
}
