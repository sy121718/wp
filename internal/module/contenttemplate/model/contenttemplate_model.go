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

	"go_wp/pkg/rls"
)

const (
	tableNameContentTemplates        = "content_templates"
	tableNameContentTemplateVersions = "content_template_versions"
	// 内容模板级组件版本锁定表（迁移 069 已把 component_id / pinned_version_id 两个外键
	// 降级为弱引用，但 template_id → content_templates(id) 的外键仍在）：删模板前必须
	// 连带清掉本表里指向它的行，否则外键会拒绝删除。
	tableNameContentTemplatePins = "content_template_component_pins"
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

// 结构模板类型（页眉 / 脚页）：标志值。
const (
	// EntityTypeHeader 页眉结构模板类型。
	EntityTypeHeader = "header"
	// EntityTypeFooter 页脚结构模板类型。
	EntityTypeFooter = "footer"
)

// IsStructureTemplateType 是否为结构模板类型（页眉 / 页脚）。
//
// 为什么是独立白名单而不是走实体来源注册表：header / footer 不是内容实体，
// 没有字段来源。往注册表里塞一个假来源，换来的是“这个类型可以配字段绑定”的假许可
// （构建期解析不到数据 → 页眉里一片空白），比拒绝更坏。
func IsStructureTemplateType(entityType string) bool {
	return entityType == EntityTypeHeader || entityType == EntityTypeFooter
}

// SetDefaultTx 事务内切换某（工程, 类型）的生效模板：旧的置 false、目标置 true。
//
// 顺序不可颠倒：部分唯一索引 idx_content_templates_default_per_project_type
// （project_id, entity_type）WHERE is_default 在“两条同时为 true”的中间态直接报冲突。
// 同事务还保证不会出现“旧的清了、新的没置上”= 该类型没有生效模板的窗口。
func (m *Model) SetDefaultTx(tx *gorm.DB, projectID, entityType, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if err := tx.Model(&TemplateEntity{}).
		Where("project_id = ? AND entity_type = ? AND is_default = true AND id <> ?", projectID, entityType, templateID).
		Updates(map[string]any{"is_default": false, "update_time": at}).Error; err != nil {
		return err
	}
	return tx.Model(&TemplateEntity{}).
		Where("id = ? AND project_id = ?", templateID, projectID).
		Updates(map[string]any{"is_default": true, "update_time": at}).Error
}

// TemplateEntity content_templates 表实体（模板草稿，可继续编辑）。
type TemplateEntity struct {
	ID         string `gorm:"column:id;primaryKey"`
	ProjectID  string `gorm:"column:project_id;not null"`
	Name       string `gorm:"column:name;not null"`
	EntityType string `gorm:"column:entity_type;not null"`
	// TemplateRole 模板角色（审计 EDT-004）：detail = 实体详情页，archive = 归档列表页。
	// 同一个实体类型下两者可以各有一套；解析时必须按角色过滤，否则归档模板会被
	// 当成详情模板被取用（画出来的页面结构完全不对）。
	TemplateRole  string          `gorm:"column:template_role;not null;default:detail"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion  int64           `gorm:"column:draft_version;not null"`
	// CurrentVersionID 当前版本指针（content_template_versions.id）。
	CurrentVersionID *string `gorm:"column:current_version_id"`
	// IsDefault 该实体类型的显式默认模板（EDT-014；每个 entity_type 至多一个）。
	IsDefault bool      `gorm:"column:is_default;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (TemplateEntity) TableName() string { return tableNameContentTemplates }

// VersionEntity content_template_versions 表实体（不可变版本快照）。
type VersionEntity struct {
	ID         string          `gorm:"column:id;primaryKey"`
	TemplateID string          `gorm:"column:template_id;not null"`
	Version    int64           `gorm:"column:version;not null"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	// SourceHash 版本文档的内容哈希（NOT NULL）。
	SourceHash string    `gorm:"column:source_hash;not null"`
	CreatedBy  string    `gorm:"column:created_by;not null"`
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
	// content_templates 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&TemplateEntity{}).Create(e).Error
	})
}

// Get 按 ID 查询模板（工程作用域内）。
//
// projectID 必填（DB-009 第二批）：content_templates 带 FORCE 策略，无作用域的
// 按 id 直查在非超级角色下静默 0 行 —— 表现为「模板不存在」，而模板其实还在，
// 只是这条路径没有告诉数据库「我是哪个工程」。
func (m *Model) Get(ctx context.Context, projectID, id string) (e *TemplateEntity, err error) {
	var row TemplateEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&TemplateEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// List 列出**本工程**模板（默认模板优先，其次更新时间倒序；entity_type 为空时返回本工程全部）。
func (m *Model) List(ctx context.Context, projectID, entityType string) (list []*TemplateEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&TemplateEntity{}).Where("project_id = ?", projectID).
			Order("is_default DESC, update_time DESC, id DESC")
		if entityType != "" {
			q = q.Where("entity_type = ?", entityType)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// ListByRole 按实体类型与**角色**列模板（审计 EDT-004）。
//
// 归档模板与详情模板可以同类型共存，解析时必须按角色过滤：混在一起时
// 归档模板会被当成详情模板取用，画出来的页面结构完全不对 —— 而构建不会报错。
// role 为空按 detail 处理（既有调用方的语义）。
func (m *Model) ListByRole(ctx context.Context, projectID, entityType, role string) (list []*TemplateEntity, err error) {
	if role == "" {
		role = TemplateRoleDetail
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&TemplateEntity{}).Where("project_id = ? AND template_role = ?", projectID, role).
			Order("is_default DESC, update_time DESC, id DESC")
		if entityType != "" {
			q = q.Where("entity_type = ?", entityType)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// Save 更新草稿（draft_document + draft_version + current_version_id + update_time）。
// 用 DB(ctx)（已绑定 Model）+ 显式 Where + Updates（避免 GORM Save
// 在已绑定 Model 下报 WHERE conditions required）。
func (m *Model) Save(ctx context.Context, projectID string, e *TemplateEntity) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&TemplateEntity{}).Where("id = ? AND project_id = ?", e.ID, projectID).Updates(map[string]any{
			"draft_document":     e.DraftDocument,
			"draft_version":      e.DraftVersion,
			"current_version_id": e.CurrentVersionID,
			"update_time":        e.UpdatedAt,
		}).Error
	})
}

// SetCurrentVersion 更新当前版本指针（版本行写入后回填）。
func (m *Model) SetCurrentVersion(ctx context.Context, projectID, templateID, versionID string, at time.Time) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&TemplateEntity{}).Where("id = ? AND project_id = ?", templateID, projectID).Updates(map[string]any{
			"current_version_id": versionID,
			"update_time":        at,
		}).Error
	})
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
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
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
func (m *Model) SaveWithVersion(ctx context.Context, projectID string, v *VersionEntity, e *TemplateEntity) error {
	// content_templates 带 FORCE 策略：新版本行落库与草稿更新同事务，且都要承本工程作用域。
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := tx.Create(v).Error; err != nil {
			return err
		}
		return tx.Model(&TemplateEntity{}).Where("id = ? AND project_id = ?", e.ID, projectID).Updates(map[string]any{
			"draft_document":     e.DraftDocument,
			"draft_version":      e.DraftVersion,
			"current_version_id": e.CurrentVersionID,
			"update_time":        e.UpdatedAt,
		}).Error
	})
}

// DeleteWithHistory 删除模板行 + 它的全部历史版本 + 组件版本锁定行（同一事务）。
//
// 三张表同属一个聚合（模板 / 版本 / 组件锁定）：留下孤儿版本行会让「模板不存在但版本还在」
// 成为永久垃圾，而版本表没有反向查找入口，事后无法清理 —— 所以删除必须是原子的。
//
// 为什么**不**级联处理 presentation_instances.template_id：那是跨聚合引用（自动发布实例
// 是用户数据），删模板顺手删掉实例是最不该发生的事。被实例引用时最后一步的外键约束会拒绝，
// 整笔事务回滚 —— 调用方据此判定「这一条不能删」，而不是让库副作用留一半。
func (m *Model) DeleteWithHistory(ctx context.Context, projectID, id string) error {
	// content_templates 带 FORCE 策略：本工程作用域既约束模板行，也让同事务里两张
	// 无 project_id 的从表操作走同一条连接。
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		// 表名是包内常量（无外部输入），id 参数化 —— 组件锁定表没有实体，用 Exec 直删。
		if err := tx.Exec("DELETE FROM "+tableNameContentTemplatePins+" WHERE template_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Where("template_id = ?", id).Delete(&VersionEntity{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&TemplateEntity{}).Error
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
