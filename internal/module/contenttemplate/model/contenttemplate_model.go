// Package contenttemplatemodel 实现 contenttemplate 模块两张表的持久化（0-A2）：
// content_templates（可编辑草稿）+ content_template_versions（不可变版本快照）。
//
// DDL 对齐（本轮修复）：实体列集合严格对齐生产 DDL
// public/migrations/init_builder_schema.sql。此前 model 缺
// content_templates.project_id / current_version_id 与
// content_template_versions.source_hash / created_by（四列均为 NOT NULL），
// 真实库下 Create 必然失败（null value in column "project_id" violates
// not-null constraint），测试靠 AutoMigrate 补列掩盖了缺陷。
package contenttemplatemodel

import (
	"context"
	"encoding/json"
	"time"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"

	"gorm.io/gorm"
)

const (
	tableNameContentTemplates        = "content_templates"
	tableNameContentTemplateVersions = "content_template_versions"
)

// 模板角色（审计 EDT-004）：真源在 contract。
//
// 它出现在契约方法 ResolveTemplateByRole 的参数位置上，跨模块调用方应当从契约取值，
// 不该为了一个字符串去 import 本模块的数据访问包。这里转发一份，供模块内部引用。
const (
	// TemplateRoleDetail 实体详情页模板（既有语义，默认值）。
	TemplateRoleDetail = contenttemplatecontract.TemplateRoleDetail
	// TemplateRoleArchive 归档列表页模板（如「分类页」：列该分类下的内容）。
	TemplateRoleArchive = contenttemplatecontract.TemplateRoleArchive
)

// IsValidTemplateRole 角色是否合法。
func IsValidTemplateRole(role string) bool {
	return role == TemplateRoleDetail || role == TemplateRoleArchive
}

// TemplateEntity content_templates 表实体（模板草稿，可继续编辑）。
type TemplateEntity struct {
	ID         string `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID  string `gorm:"column:project_id;type:uuid;not null"`
	Name       string `gorm:"column:name;not null"`
	EntityType string `gorm:"column:entity_type;not null"`
	// TemplateRole 模板角色（审计 EDT-004）：detail = 实体详情页，archive = 归档列表页。
	// 同一个实体类型下两者可以各有一套；解析时必须按角色过滤，否则归档模板会被
	// 当成详情模板被取用（画出来的页面结构完全不对）。
	TemplateRole  string          `gorm:"column:template_role;not null;default:detail"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion  int64           `gorm:"column:draft_version;not null"`
	// CurrentVersionID 当前版本指针（content_template_versions.id）。
	CurrentVersionID *string `gorm:"column:current_version_id;type:uuid"`
	// IsDefault 该实体类型的显式默认模板（EDT-014；每个 entity_type 至多一个）。
	IsDefault bool      `gorm:"column:is_default;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (TemplateEntity) TableName() string { return tableNameContentTemplates }

// VersionEntity content_template_versions 表实体（不可变版本快照）。
type VersionEntity struct {
	ID         string          `gorm:"column:id;type:uuid;primaryKey"`
	TemplateID string          `gorm:"column:template_id;type:uuid;not null"`
	Version    int64           `gorm:"column:version;not null"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	// SourceHash 版本文档的内容哈希（NOT NULL）。
	SourceHash string    `gorm:"column:source_hash;not null"`
	CreatedBy  string    `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
}

// TableName 表名。
func (VersionEntity) TableName() string { return tableNameContentTemplateVersions }

// Model contenttemplate 两张表的数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定模板表的查询入口。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&TemplateEntity{})
}

// DBVersion 绑定版本表的查询入口。
func (m *Model) DBVersion(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VersionEntity{})
}

// Transaction 透传事务（service 编排跨两张表的原子写入）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// Create 新增模板草稿。
func (m *Model) Create(ctx context.Context, e *TemplateEntity) error {
	return m.DB(ctx).Create(e).Error
}

// Get 按 ID 查询模板。
func (m *Model) Get(ctx context.Context, id string) (e *TemplateEntity, err error) {
	var row TemplateEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 按 entity_type 列表（默认模板优先，其次更新时间倒序；entity_type 为空时返回全部）。
func (m *Model) List(ctx context.Context, entityType string) (list []*TemplateEntity, err error) {
	q := m.db.WithContext(ctx).Order("is_default DESC, update_time DESC, id DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	err = q.Find(&list).Error
	return list, err
}

// ListByRole 按实体类型与**角色**列模板（审计 EDT-004）。
//
// 归档模板与详情模板可以同类型共存，解析时必须按角色过滤：混在一起时
// 归档模板会被当成详情模板取用，画出来的页面结构完全不对 —— 而构建不会报错。
// role 为空按 detail 处理（既有调用方的语义）。
func (m *Model) ListByRole(ctx context.Context, entityType, role string) (list []*TemplateEntity, err error) {
	if role == "" {
		role = TemplateRoleDetail
	}
	q := m.db.WithContext(ctx).Where("template_role = ?", role).
		Order("is_default DESC, update_time DESC, id DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	err = q.Find(&list).Error
	return list, err
}

// Save 更新草稿（draft_document + draft_version + current_version_id + update_time）。
// 用 DB(ctx)（已绑定 Model）+ 显式 Where + Updates（避免 GORM Save
// 在已绑定 Model 下报 WHERE conditions required）。
func (m *Model) Save(ctx context.Context, e *TemplateEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"draft_document":     e.DraftDocument,
		"draft_version":      e.DraftVersion,
		"current_version_id": e.CurrentVersionID,
		"update_time":        e.UpdatedAt,
	}).Error
}

// SetCurrentVersion 更新当前版本指针（版本行写入后回填）。
func (m *Model) SetCurrentVersion(ctx context.Context, templateID, versionID string, at time.Time) error {
	return m.DB(ctx).Where("id = ?", templateID).Updates(map[string]any{
		"current_version_id": versionID,
		"update_time":        at,
	}).Error
}

// CreateVersion 写入不可变版本快照。
func (m *Model) CreateVersion(ctx context.Context, v *VersionEntity) error {
	return m.DBVersion(ctx).Create(v).Error
}

// CreateWithVersion 同一事务内写模板行 + 首个版本行 + 回填当前版本指针。
//
// 三步必须原子：任一中间步骤失败会留下「模板存在但没有 LatestVersion」或
// 「版本行在、指针为空」的死模板 —— ResolveTemplate 直接失败且无法自愈
// （重试也撞版本唯一索引）。属聚合内原子组合，事务边界留在 model。
func (m *Model) CreateWithVersion(ctx context.Context, e *TemplateEntity, v *VersionEntity) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		if err := tx.Create(v).Error; err != nil {
			return err
		}
		return tx.Model(&TemplateEntity{}).Where("id = ?", e.ID).
			Updates(map[string]any{
				"current_version_id": v.ID,
				"update_time":        e.UpdatedAt,
			}).Error
	})
}

// SaveWithVersion 同一事务内写新版本行 + 更新草稿与当前版本指针。
//
// 版本行先落库而草稿 Save 失败时，指针停在旧版本，新版本成为不可达孤儿，
// 且 draft_version 已自增导致重试版本号错位。
func (m *Model) SaveWithVersion(ctx context.Context, v *VersionEntity, e *TemplateEntity) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(v).Error; err != nil {
			return err
		}
		return tx.Model(&TemplateEntity{}).Where("id = ?", e.ID).Updates(map[string]any{
			"draft_document":     e.DraftDocument,
			"draft_version":      e.DraftVersion,
			"current_version_id": e.CurrentVersionID,
			"update_time":        e.UpdatedAt,
		}).Error
	})
}

// LatestVersion 取模板最新版本（version 降序首条）。
func (m *Model) LatestVersion(ctx context.Context, templateID string) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("template_id = ?", templateID).
		Order("version DESC").
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetVersion 按模板 ID + 版本号取指定版本。
func (m *Model) GetVersion(ctx context.Context, templateID string, version int64) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("template_id = ? AND version = ?", templateID, version).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
