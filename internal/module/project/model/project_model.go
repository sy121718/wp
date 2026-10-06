// Package projectmodel 实现 project 模块 projects 表持久化。
package projectmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const tableNameProjects = "projects"

// ProjectEntity 对应 projects 表。
type ProjectEntity struct {
	ID        string          `gorm:"column:id;primaryKey"`
	Name      string          `gorm:"column:name;not null"`
	Settings  json.RawMessage `gorm:"column:settings;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
	// AnalyticsRetentionDays page_views 明细保留天数（迁移 161；0 = 不清理）。
	//
	// `->` 是**只读权限标签**：GORM 会 SELECT 它，但绝不把它写进 INSERT / UPDATE。
	// 少了这个标签，本模块任何一次 Create 都会把它当零值插进去，把建表默认值 90 覆盖成 0
	//（= 永不清理）—— 这是「删数据」方向的静默失效：没人会收到报错，只是访问明细
	// 从此永远留在库里，几个月后才发现表大得离谱。本列目前没有写入方（只能改 SQL），
	// 所以只读映射不丢任何能力。
	AnalyticsRetentionDays int `gorm:"column:analytics_retention_days;->"`
}

func (ProjectEntity) TableName() string { return tableNameProjects }

// Model projects 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewProjectModel 创建 Project Model。
func NewProjectModel(db *gorm.DB) *Model {
	return &Model{db: db}
}

// DB 返回已绑定 projects 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProjectEntity{})
}

// ThemeDB 返回已绑定 themes 表的 GORM 实例。
// 主题查询必须走本绑定：混用 DB(ctx)（已绑定 projects）会让 GORM
// 把 ThemeEntity 字段投影到 projects 表上，产生错误 SQL。
func (m *Model) ThemeDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ThemeEntity{})
}

// Create 新增工程。
func (m *Model) Create(ctx context.Context, e *ProjectEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// ListAll 按创建时间列出全部项目。
func (m *Model) ListAll(ctx context.Context) (list []ProjectEntity, err error) {
	err = m.DB(ctx).Order("create_time ASC").Find(&list).Error
	return list, err
}

// GetByID 按 ID 查询工程。
func (m *Model) GetByID(ctx context.Context, id string) (e *ProjectEntity, err error) {
	e = &ProjectEntity{}
	if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// Update 按 ID 更新工程名称与设置。
func (m *Model) Update(ctx context.Context, id, name string, settings json.RawMessage, updatedAt time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"name": name, "settings": settings, "update_time": updatedAt,
	}).Error
}

// ListRetentionPolicies 列出启用了访问明细自动清理的工程（保留期 > 0）。
//
// 排序与 ListAll 同口径（create_time ASC, id ASC）：保留期清理逐工程跑，顺序稳定
// 才好对日志与耗时。
//
// 表的所有权在 project 模块，所以「读 projects 的某一列」也留在本模块 —— analytics 的
// 保留期清理经契约取这份清单，而不是越过模块边界直接查这张表。
func (m *Model) ListRetentionPolicies(ctx context.Context) (list []ProjectEntity, err error) {
	err = m.DB(ctx).
		Where("analytics_retention_days > 0").
		Order("create_time ASC, id ASC").
		Find(&list).Error
	return list, err
}
