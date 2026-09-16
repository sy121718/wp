package pagemodel

// page_redirect_model.go — 重定向路由（page_routes 的 redirect 行）的最小读写面。
//
// 表所有权说明：page_routes 属 publication 模块（docs/02-domain.md §page_routes），
// 「激活 / 取消激活 / 标记重定向 / 按实体批量删除」都经 pubcontract 走。
// 本文件只补两个 contract 尚未提供、而 SEO-025 的重定向管理页必须用到的最小能力：
//   · 列出本工程全部 redirect 行（回答「当前有哪些 301」）；
//   · 删除单条 redirect 行并读取某路径的 active 行（回答「这条能不能删、目标是哪条」）。
//
// 为什么落在 page 模块：管理页挂在本模块的路由组下（authorizedAPI），而本批不允许改动
// publication 模块。后续应收进 publication contract，本文件随之退化为纯调用方 ——
// 见 SEO-025 的遗留问题记录。

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNamePageRoutes = "page_routes"

// page_routes.route_kind 的白名单值（与 publication 模块 model 常量同义，
// 跨模块不 import 对方 model，此处按表定义独立声明）。
const (
	// RouteKindActive 已激活（该路径直出产物）。
	RouteKindActive = "active"
	// RouteKindRedirect 旧路径 301（该路径的激活产物是 redirect.json）。
	RouteKindRedirect = "redirect"
)

// PageRouteEntity 对应 page_routes 行（只映射本模块用到的列）。
type PageRouteEntity struct {
	ProjectID      string    `gorm:"column:project_id;type:uuid;primaryKey"`
	Path           string    `gorm:"column:path;type:text;primaryKey"`
	PageID         *string   `gorm:"column:page_id;type:uuid"`
	PresentationID *string   `gorm:"column:presentation_id;type:uuid"`
	RouteKind      string    `gorm:"column:route_kind;type:text;not null"`
	ArtifactID     *string   `gorm:"column:artifact_id;type:uuid"`
	UpdatedAt      time.Time `gorm:"column:update_time;not null"`
}

func (PageRouteEntity) TableName() string { return tableNamePageRoutes }

// RouteDB 返回已绑定 page_routes 表的 GORM 实例。
func (m *Model) RouteDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageRouteEntity{})
}

// ListRedirectRoutes 列出工程下全部重定向行（路径升序，确定性输出）。
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

// ListActiveRoutesByProject 列出工程下全部已激活路径行（路径升序）。
//
// 重定向管理页要用它回答两件事：某条目标路径是否真实存在（route_kind=active），
// 以及该目标归属哪个页面 / 展示实例（新增重定向时 page_routes 的归属列必须恰好一个非空）。
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

// GetActiveRouteByPath 按路径取已激活路由行（判断「目标路径是否真实存在」）；
// 不存在返回 gorm.ErrRecordNotFound。
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

// GetRedirectRoute 按路径取重定向行；不存在返回 gorm.ErrRecordNotFound。
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

// DeleteRedirectRoute 删除单条重定向行（只删 route_kind=redirect 的行，
// 绝不触碰同路径上的其它行或目标实体的 active/reserved 行）。
// 返回受影响行数（0 = 本来就没有）。
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
