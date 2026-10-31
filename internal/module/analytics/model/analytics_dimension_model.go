package analyticsmodel

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const (
	DimensionReferrer = "referrer"

	DimensionUA = "ua"

	DimensionLang = "lang"
)

var dimensionColumns = map[string]string{
	DimensionReferrer: "referrer_host",
	DimensionUA:       "ua_class",
	DimensionLang:     "lang",
}

type DimensionRow struct {
	Value    string
	Views    int64
	Visitors int64
}

func (m *Model) CountByDimension(ctx context.Context, projectID string, from, to time.Time,
	dimension string, limit int) (rows []DimensionRow, err error) {
	column, ok := dimensionColumns[dimension]
	if !ok {

		return nil, fmt.Errorf("未知的统计维度: %q", dimension)
	}
	if limit <= 0 {

		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select(column+" AS value, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group(column).Order("views DESC, value ASC").
			Limit(limit).Scan(&rows).Error
	})
	return rows, err
}
