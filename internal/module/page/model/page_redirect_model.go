package pagemodel

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNamePageRoutes = "page_routes"

const (
	RouteKindActive = "active"

	RouteKindRedirect = "redirect"
)

type PageRouteEntity struct {
	ProjectID      string    `gorm:"column:project_id;primaryKey"`
	Path           string    `gorm:"column:path;primaryKey"`
	PageID         *string   `gorm:"column:page_id"`
	PresentationID *string   `gorm:"column:presentation_id"`
	RouteKind      string    `gorm:"column:route_kind;not null"`
	ArtifactID     *string   `gorm:"column:artifact_id"`
	UpdatedAt      time.Time `gorm:"column:update_time;not null"`
}

func (PageRouteEntity) TableName() string { return tableNamePageRoutes }

func (m *Model) ListRedirectRoutes(ctx context.Context, projectID string) (list []PageRouteEntity, err error) {
	if projectID == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageRouteEntity{}).
			Where("project_id = ? AND route_kind = ?", projectID, RouteKindRedirect).
			Order("path ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) ListActiveRoutesByProject(ctx context.Context, projectID string) (list []PageRouteEntity, err error) {
	if projectID == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageRouteEntity{}).
			Where("project_id = ? AND route_kind = ?", projectID, RouteKindActive).
			Order("path ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) GetActiveRouteByPath(ctx context.Context, projectID, path string) (e *PageRouteEntity, err error) {
	e = &PageRouteEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageRouteEntity{}).
			Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, RouteKindActive).
			First(e).Error
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) GetRedirectRoute(ctx context.Context, projectID, path string) (e *PageRouteEntity, err error) {
	e = &PageRouteEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageRouteEntity{}).
			Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, RouteKindRedirect).
			First(e).Error
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) DeleteRedirectRoute(ctx context.Context, projectID, path string) (n int64, err error) {
	var res *gorm.DB
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res = tx.Model(&PageRouteEntity{}).
			Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, RouteKindRedirect).
			Delete(&PageRouteEntity{})
		return res.Error
	})
	if err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}

func (m *Model) DeleteRedirectRouteTx(ctx context.Context, tx *gorm.DB, projectID, path string) (n int64, err error) {
	if tx == nil {
		return 0, errors.New("page: 删除重定向行必须在调用方事务内")
	}
	res := tx.WithContext(ctx).Model(&PageRouteEntity{}).
		Where("project_id = ? AND path = ? AND route_kind = ?", projectID, path, RouteKindRedirect).
		Delete(&PageRouteEntity{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
