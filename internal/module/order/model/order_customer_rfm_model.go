package model

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func orderRfmScored(tx *gorm.DB, projectID string, from, to time.Time) *gorm.DB {
	statuses := strings.Join(paidStatuses, ",")
	agg := tx.Table(OrderEntity{}.TableName()+" AS o").
		Select("o.user_id, MAX(o.create_time) AS last_at, "+
			"COUNT(*) FILTER (WHERE o.create_time >= ? AND o.create_time < ?) AS freq, "+
			"COALESCE(SUM("+orderNetTotalSQLExpr+") FILTER (WHERE o.create_time >= ? AND o.create_time < ?), 0) AS monetary",
			from, to, strings.Join(ReturnedStatuses, ","), from, to).
		Where("o.project_id = ?", projectID).
		Where("o.status = ANY(string_to_array(?, ',')::text[])", statuses).
		Where("o.user_id IS NOT NULL").
		Group("o.user_id")
	return tx.Table("(?) AS a", agg).
		Select("a.user_id, a.last_at, a.freq, a.monetary, " +
			"6 - NTILE(5) OVER (ORDER BY a.last_at DESC, a.user_id) AS r_score, " +
			"NTILE(5) OVER (ORDER BY a.freq ASC, a.user_id) AS f_score, " +
			"NTILE(5) OVER (ORDER BY a.monetary ASC, a.user_id) AS m_score").
		Where("a.freq > 0")
}

const rfmSegmentExpr = `CASE WHEN (r_score + f_score + m_score) >= 12 THEN 'vip'
                             WHEN (r_score + f_score + m_score) >= 8 THEN 'potential'
                             ELSE 'low_value' END`

const rfmSummarySelect = "COUNT(*) AS customers, " +
	"COUNT(*) FILTER (WHERE segment = 'vip') AS vip, " +
	"COUNT(*) FILTER (WHERE segment = 'potential') AS potential, " +
	"COUNT(*) FILTER (WHERE segment = 'low_value') AS low_value"

const (
	RfmSegmentVip       = "vip"
	RfmSegmentPotential = "potential"
	RfmSegmentLowValue  = "low_value"
)

func IsKnownRfmSegment(segment string) bool {
	switch segment {
	case RfmSegmentVip, RfmSegmentPotential, RfmSegmentLowValue:
		return true
	}
	return false
}

type OrderCustomerRfmRow struct {
	UserID     int64     `gorm:"column:user_id"`
	LastAt     time.Time `gorm:"column:last_at"`
	Freq       int64     `gorm:"column:freq"`
	Monetary   int64     `gorm:"column:monetary"`
	RScore     int       `gorm:"column:r_score"`
	FScore     int       `gorm:"column:f_score"`
	MScore     int       `gorm:"column:m_score"`
	TotalScore int       `gorm:"column:total_score"`
	Segment    string    `gorm:"column:segment"`
}

type OrderCustomerRfmSegmentRow struct {
	Customers int64 `gorm:"column:customers"`
	Vip       int64 `gorm:"column:vip"`
	Potential int64 `gorm:"column:potential"`
	LowValue  int64 `gorm:"column:low_value"`
}

const rfmRowColumns = ` user_id, last_at, freq, monetary, r_score, f_score, m_score,
       (r_score + f_score + m_score) AS total_score,
       ` + rfmSegmentExpr + ` AS segment`

func (m *OrderModel) CustomerRfmSummary(ctx context.Context, projectID string, from, to time.Time) (row OrderCustomerRfmSegmentRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return row, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return row, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		scored := orderRfmScored(tx, projectID, from, to)
		inner := tx.Table("(?) AS scored", scored).Select(rfmRowColumns)
		return tx.Table("(?) AS s", inner).
			Select(rfmSummarySelect).
			Scan(&row).Error
	})
	return row, err
}

func (m *OrderModel) CustomerRfmList(ctx context.Context, projectID string, from, to time.Time, segment string, limit, offset int) (rows []OrderCustomerRfmRow, total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, 0, ErrRangeRequired
	}
	if segment != "" && !IsKnownRfmSegment(segment) {
		return nil, 0, ErrSegmentUnknown
	}
	if limit <= 0 {
		return nil, 0, ErrRangeRequired
	}

	type listRow struct {
		UserID     int64     `gorm:"column:user_id"`
		LastAt     time.Time `gorm:"column:last_at"`
		Freq       int64     `gorm:"column:freq"`
		Monetary   int64     `gorm:"column:monetary"`
		RScore     int       `gorm:"column:r_score"`
		FScore     int       `gorm:"column:f_score"`
		MScore     int       `gorm:"column:m_score"`
		TotalScore int       `gorm:"column:total_score"`
		Segment    string    `gorm:"column:segment"`
		Total      int64     `gorm:"column:total"`
	}
	var raw []listRow
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		scored := orderRfmScored(tx, projectID, from, to)
		inner := tx.Table("(?) AS scored", scored).Select(rfmRowColumns)
		q := tx.Table("(?) AS s", inner).Select("*, COUNT(*) OVER () AS total")
		if segment != "" {

			q = q.Where("s.segment = ?", segment)
		}
		return q.Order("s.total_score DESC, s.user_id").Limit(limit).Offset(offset).Scan(&raw).Error
	})
	if err != nil {
		return nil, 0, err
	}
	rows = make([]OrderCustomerRfmRow, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, OrderCustomerRfmRow{
			UserID: r.UserID, LastAt: r.LastAt, Freq: r.Freq, Monetary: r.Monetary,
			RScore: r.RScore, FScore: r.FScore, MScore: r.MScore,
			TotalScore: r.TotalScore, Segment: r.Segment,
		})
		total = r.Total
	}
	return rows, total, nil
}

func (m *OrderModel) CustomerRfmSegmentIDs(ctx context.Context, projectID string, from, to time.Time, segment string, limit, offset int) (ids []int64, total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, 0, ErrRangeRequired
	}
	if !IsKnownRfmSegment(segment) {
		return nil, 0, ErrSegmentUnknown
	}
	if limit <= 0 {
		return nil, 0, ErrRangeRequired
	}

	type idRow struct {
		UserID int64 `gorm:"column:user_id"`
		Total  int64 `gorm:"column:total"`
	}
	var raw []idRow
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		scored := orderRfmScored(tx, projectID, from, to)
		inner := tx.Table("(?) AS scored", scored).Select(rfmRowColumns)
		return tx.Table("(?) AS s", inner).
			Select("s.user_id, COUNT(*) OVER () AS total").
			Where("s.segment = ?", segment).
			Order("s.total_score DESC, s.user_id").
			Limit(limit).Offset(offset).
			Scan(&raw).Error
	})
	if err != nil {
		return nil, 0, err
	}
	ids = make([]int64, 0, len(raw))
	for _, r := range raw {
		ids = append(ids, r.UserID)
		total = r.Total
	}
	return ids, total, nil
}
