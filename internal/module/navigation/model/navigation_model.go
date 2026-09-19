// Package navigationmodel 实现 navigation 模块 navigations 表持久化（0-C）。
// navigations 为公开站点导航表，与后台权限菜单 sys_menus 严格隔离，不可复用同一张表。
package navigationmodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNameNavigations = "navigations"

// NavigationEntity 对应 navigations 表。
type NavigationEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Title     string `gorm:"column:title;not null"`
	Path      string `gorm:"column:path;not null"`
	Kind      string `gorm:"column:kind;not null"`
	// SourceType / SourceID 菜单项来源（custom/page/article/product/category/block）。
	SourceType string  `gorm:"column:source_type;not null;default:custom"`
	SourceID   *string `gorm:"column:source_id"`
	// Target 打开方式：self / blank。
	Target string `gorm:"column:target;not null;default:self"`
	// PanelBlockID 悬浮面板引用的全局块（超级菜单；NULL = 无面板，迁移 285）。
	//
	// 与 SourceType='block' 是两件事：那个是"点这一项跳到哪"（链接来源），
	// 这个是"悬停展开显示什么"（面板内容）。
	PanelBlockID *string `gorm:"column:panel_block_id"`
	// PanelWidth 面板展示宽度 auto / full（通栏）；移动端由样式强制全宽。
	PanelWidth string    `gorm:"column:panel_width;not null;default:auto"`
	ParentID   *string   `gorm:"column:parent_id"`
	SortOrder  int       `gorm:"column:sort_order;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time `gorm:"column:update_time;not null"`
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

// ListAllProjectIDs 列出全部站点工程 id（只带 id 的入口做逐工程定位时的兜底清单）。
//
// 为什么 navigation model 要读 projects 表（DB-009）：「按 id 更新 / 查询 / 删除」这三个
// 入口的请求里只有 id，而 navigations 在迁移 215 里带 FORCE 策略 —— 作用域只能落到某个
// 具体工程，归属就必须先探测出来。探测要枚举工程清单，清单来自 project 契约；契约未注入时
// （测试装配，或将来某个装配点漏接）定位会整体失败，表现为「导航项明明在却报不存在」，
// 而日志里什么都没有。page / order model 的 ListAllProjectIDs 是同一处境的同形兜底。
//
// projects 是隔离的**主体**：它没有 project_id 列、不在迁移 215 的 53 个对象里，
// 读它不涉及任何被隔离数据。正确做法仍是装配点注入 project 契约（生产装配已注入）。
func (m *Model) ListAllProjectIDs(ctx context.Context) (ids []string, err error) {
	err = m.db.WithContext(ctx).
		Raw("SELECT id::text FROM projects ORDER BY create_time ASC, id ASC").Scan(&ids).Error
	return ids, err
}

// Create 新增导航项。
// RLS（迁移 215）：navigations 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *Model) Create(ctx context.Context, e *NavigationEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).Create(e).Error
	})
}

// Get 按 ID 查询导航项。
//
// projectID 非空时在工程作用域内查（RLS 变量 + 显式 project_id 条件）；为空沿用
// 「不限工程」的历史调用形态 —— 不设 scope 时换非超级角色后该路径 fail closed
// （0 行 → ErrRecordNotFound），不会读到别的工程，方向是安全的；要让它可用
// 必须由调用方补 projectID（DB-009 剩余清单）。
func (m *Model) Get(ctx context.Context, projectID, id string) (e *NavigationEntity, err error) {
	var row NavigationEntity
	if strings.TrimSpace(projectID) == "" {
		if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
			return nil, err
		}
		return &row, nil
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(&row).Error
	}); err != nil {
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
// projectID 非空时在工程作用域内写（越界写会被 WITH CHECK 直接拒绝而不是静默改到别的工程）。
func (m *Model) Save(ctx context.Context, projectID, id string, updates map[string]any) error {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Updates(updates).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).Updates(updates).Error
	})
}

// Delete 按 ID 删除导航项。
// projectID 非空时在工程作用域内删（越界删在换角色后会被策略拒绝，而不是删掉别的工程）。
func (m *Model) Delete(ctx context.Context, projectID, id string) error {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Delete(&NavigationEntity{}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).Delete(&NavigationEntity{}).Error
	})
}

// DeleteMany 批量删除导航项（同一聚合内：删菜单项及其全部子孙）。
func (m *Model) DeleteMany(ctx context.Context, projectID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id IN ?", ids).Delete(&NavigationEntity{}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("project_id = ? AND id IN ?", projectID, ids).Delete(&NavigationEntity{}).Error
	})
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
