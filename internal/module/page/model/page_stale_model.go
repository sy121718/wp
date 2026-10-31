package pagemodel

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const staleTitleExpr = "COALESCE(NULLIF(BTRIM(draft_document #>> '{settings,seo,title}'), ''), '')"

const (
	StaleOrderUpdateTime = "update_time"

	StaleOrderDraftPath = "draft_path"

	StaleOrderTitle = "title"

	StaleOrderID = "id"
)

const (
	DefaultStaleListLimit = 50

	MaxStaleListLimit = 500
)

var ErrInvalidStaleOrder = errors.New("page: 待重建清单的排序键不在白名单里")

var staleOrderKeys = map[string]struct{}{
	StaleOrderUpdateTime: {},
	StaleOrderDraftPath:  {},
	StaleOrderTitle:      {},
	StaleOrderID:         {},
}

func NormalizeStaleOrder(orderBy string) (string, error) {
	key := strings.TrimSpace(orderBy)
	if key == "" {
		return StaleOrderUpdateTime, nil
	}
	if _, ok := staleOrderKeys[key]; !ok {
		return "", ErrInvalidStaleOrder
	}
	return key, nil
}

func NormalizeStaleListLimit(limit int) int {
	if limit <= 0 {
		return DefaultStaleListLimit
	}
	if limit > MaxStaleListLimit {
		return MaxStaleListLimit
	}
	return limit
}

type StalePageRow struct {
	ID        string `gorm:"column:id"`
	ProjectID string `gorm:"column:project_id"`
	Title     string `gorm:"column:title"`
	DraftPath string `gorm:"column:draft_path"`

	ActivePath *string   `gorm:"column:active_path"`
	Stale      bool      `gorm:"column:stale"`
	UpdatedAt  time.Time `gorm:"column:update_time"`

	RebuildFailedAt    *time.Time `gorm:"column:rebuild_failed_at"`
	RebuildFailedStage *string    `gorm:"column:rebuild_failed_stage"`
}

func stalePageSelect() string {
	return "id, project_id, " + staleTitleExpr + " AS title, draft_path, active_path, stale, update_time, " +
		"rebuild_failed_at, rebuild_failed_stage"
}

func staleOrderClause(column string, descending bool) string {
	dir := "ASC"
	if descending {
		dir = "DESC"
	}
	return column + " " + dir + ", id ASC"
}

func (m *Model) ListStale(ctx context.Context, projectID string, limit int, orderBy string, descending bool) (list []StalePageRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	key, oerr := NormalizeStaleOrder(orderBy)
	if oerr != nil {
		return nil, oerr
	}
	column := key
	limit = NormalizeStaleListLimit(limit)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select(stalePageSelect()).
			Where("project_id = ? AND deleted_at IS NULL AND stale = true", projectID).
			Order(staleOrderClause(column, descending)).
			Limit(limit).
			Scan(&list).Error
	})
	return list, err
}

func (m *Model) CountStale(ctx context.Context, projectID string) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND stale = true", projectID).
			Count(&n).Error
	})
	return n, err
}

func (m *Model) ListStaleIDs(ctx context.Context, projectID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	ids = make([]string, 0)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND stale = true", projectID).
			Order("id ASC").
			Pluck("id", &ids).Error
	})
	return ids, err
}

func (m *Model) ListBriefsByIDs(ctx context.Context, projectID string, ids []string, limit int, orderBy string, descending bool) (list []StalePageRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	key, oerr := NormalizeStaleOrder(orderBy)
	if oerr != nil {
		return nil, oerr
	}
	column := key
	limit = NormalizeStaleListLimit(limit)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select(stalePageSelect()).
			Where("project_id = ? AND id IN ? AND deleted_at IS NULL", projectID, ids).
			Order(staleOrderClause(column, descending)).
			Limit(limit).
			Scan(&list).Error
	})
	return list, err
}

func (m *Model) MarkRebuildFailure(ctx context.Context, projectID, pageID, stage string, at time.Time) error {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return m.db.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ?", pageID, projectID).
		Updates(map[string]any{
			"rebuild_failed_at":    at.UTC(),
			"rebuild_failed_stage": stage,
		}).Error
}

func (m *Model) ClearRebuildFailure(ctx context.Context, projectID, pageID string) error {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return m.db.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ?", pageID, projectID).
		Updates(map[string]any{
			"rebuild_failed_at":    nil,
			"rebuild_failed_stage": nil,
		}).Error
}
