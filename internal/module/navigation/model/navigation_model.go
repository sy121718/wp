// Package navigationmodel 实现 navigation 模块 navigations 表持久化（0-C）。
// navigations 为公开站点导航表，与后台权限菜单 sys_menus 严格隔离，不可复用同一张表。
package navigationmodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNameNavigations = "navigations"

// NavigationEntity 对应 navigations 表。
type NavigationEntity struct {
	ID        string `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string `gorm:"column:project_id;type:uuid;not null"`
	Title     string `gorm:"column:title;not null"`
	Path      string `gorm:"column:path;not null"`
	Kind      string `gorm:"column:kind;not null"`
	// SourceType / SourceID 菜单项来源（custom/page/article/product/category/block）。
	SourceType string  `gorm:"column:source_type;not null;default:custom"`
	SourceID   *string `gorm:"column:source_id;type:uuid"`
	// Target 打开方式：self / blank。
	Target    string    `gorm:"column:target;not null;default:self"`
	ParentID  *string   `gorm:"column:parent_id;type:uuid"`
	SortOrder int       `gorm:"column:sort_order;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (NavigationEntity) TableName() string { return tableNameNavigations }

// Model navigations 表数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 navigations 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&NavigationEntity{})
}

// Create 新增导航项。
// RLS（迁移 215）：navigations 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *Model) Create(ctx context.Context, e *NavigationEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).Create(e).Error
	})
}

// Get 按 ID 查询导航项。
func (m *Model) Get(ctx context.Context, id string) (e *NavigationEntity, err error) {
	var row NavigationEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 按工程（可选 kind）列出导航项，sort_order 升序、同序按 id 升序。
func (m *Model) List(ctx context.Context, projectID, kind string) (list []*NavigationEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&NavigationEntity{}).Where("project_id = ?", projectID)
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		return q.Order("sort_order ASC, id ASC").Find(&list).Error
	})
	return list, err
}

// MaxSortOrder 返回同工程同 kind 同父级下的最大排序值（无记录返回 0）。
// 供 service 在未显式指定排序时把新项追加到末尾。
func (m *Model) MaxSortOrder(ctx context.Context, projectID, kind string, parentID *string) (maxOrder int, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&NavigationEntity{}).Where("project_id = ? AND kind = ?", projectID, kind)
		if parentID == nil || *parentID == "" {
			q = q.Where("parent_id IS NULL")
		} else {
			q = q.Where("parent_id = ?", *parentID)
		}
		return q.Select("COALESCE(MAX(sort_order), 0)").Row().Scan(&maxOrder)
	})
	return maxOrder, err
}

// Save 按 ID 部分更新（Where("id = ?").Updates(map)）。
func (m *Model) Save(ctx context.Context, id string, updates map[string]any) error {
	return m.DB(ctx).Where("id = ?", id).Updates(updates).Error
}

// Delete 按 ID 删除导航项。
func (m *Model) Delete(ctx context.Context, id string) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&NavigationEntity{}).Error
}

// DeleteMany 批量删除导航项（同一聚合内：删菜单项及其全部子孙）。
func (m *Model) DeleteMany(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return m.DB(ctx).Where("id IN ?", ids).Delete(&NavigationEntity{}).Error
}

// ExistsPath 判断同工程同 kind 下 path 是否已被（其他）导航项占用。
// excludeID 非空时排除自身，供更新场景复用。
func (m *Model) ExistsPath(ctx context.Context, projectID, kind, path, excludeID string) (bool, error) {
	var count int64
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&NavigationEntity{}).Where("project_id = ? AND kind = ? AND path = ?", projectID, kind, path)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		return q.Count(&count).Error
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
