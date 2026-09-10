// Package pagemodel 实现 page 模块 pages、page_revisions 与 page_routes 表持久化。
package pagemodel

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrDraftVersionConflict 表示乐观锁更新未命中当前草稿版本。
	ErrDraftVersionConflict = errors.New("page 草稿版本冲突")
)

const (
	tableNamePages         = "pages"
	tableNamePageRevisions = "page_revisions"
)

// PageEntity 对应 pages 表的手工 Page 字段。
type PageEntity struct {
	ID                string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID         string          `gorm:"column:project_id;type:uuid;not null"`
	ThemeID           *string         `gorm:"column:theme_id;type:uuid"`
	Kind              string          `gorm:"column:kind;type:text;not null"`
	ContentTargetType string          `gorm:"column:content_target_type;type:text;not null"`
	ContentTargetID   *string         `gorm:"column:content_target_id;type:uuid"`
	DraftPath         string          `gorm:"column:draft_path;type:text;not null"`
	ActivePath        *string         `gorm:"column:active_path;type:text"`
	DraftDocument     json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion      int64           `gorm:"column:draft_version;not null"`
	StagedArtifactID  *string         `gorm:"column:staged_artifact_id;type:uuid"`
	ActiveArtifactID  *string         `gorm:"column:active_artifact_id;type:uuid"`
	Stale             bool            `gorm:"column:stale;not null"`
	DeletedAt         *time.Time      `gorm:"column:deleted_at"`
	PublishedAt       *time.Time      `gorm:"column:published_at"`
	CreatedAt         time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt         time.Time       `gorm:"column:updated_at;not null"`
}

func (PageEntity) TableName() string { return tableNamePages }

// RevisionEntity 对应 page_revisions 表：每次保存的不可变草稿快照。
type RevisionEntity struct {
	ID            string          `gorm:"column:id;type:uuid;primaryKey"`
	PageID        string          `gorm:"column:page_id;type:uuid;not null"`
	Version       int64           `gorm:"column:version;not null"`
	DraftPath     string          `gorm:"column:draft_path;type:text;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	SourceHash    string          `gorm:"column:source_hash;type:text;not null"`
	CreatedAt     time.Time       `gorm:"column:created_at;not null"`
}

func (RevisionEntity) TableName() string { return tableNamePageRevisions }

// Model 封装 page 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewPageModel 创建 Page Model。
func NewPageModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 pages 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageEntity{})
}

// RevisionDB 返回已绑定 page_revisions 表的 GORM 实例。
func (m *Model) RevisionDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RevisionEntity{})
}

// Transaction 在数据库事务中执行给定函数；草稿、修订与路径占用必须原子提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ListAll 列出未删除页面（排除大字段 draft_document，供列表页使用）。
// themeID 为空时列全部；非空时只列挂在该主题下的页面（020_themes.sql：主题下面才是页面）。
func (m *Model) ListAll(ctx context.Context, themeID string) (list []PageEntity, err error) {
	q := m.DB(ctx).Omit("draft_document").Where("deleted_at IS NULL")
	if themeID != "" {
		q = q.Where("theme_id = ?", themeID)
	}
	err = q.Order("updated_at DESC, id DESC").Find(&list).Error
	return list, err
}

// ListDraftDocuments 列出全部未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描用）。
//
// 与 ListAll 的区别：带 draft_document 大字段（工作台要按组件白名单收集候选，
// 无法在 SQL 侧完成——白名单在 Go 里）；按 updated_at 倒序，便于诊断。
// 代价：一次查询返回全站草稿 JSONB，调用方必须自带缓存与页数上限（见 dashboard 工作台）。
func (m *Model) ListDraftDocuments(ctx context.Context) (list []PageEntity, err error) {
	err = m.DB(ctx).
		Select("id", "project_id", "draft_path", "draft_document", "updated_at").
		Where("deleted_at IS NULL").
		Order("updated_at DESC, id DESC").
		Find(&list).Error
	return list, err
}

// RefreshThemeForTheme 把主题设置批量合入挂在该主题下全部页面的 settings.theme。
// 使用 jsonb_set 只替换 settings.theme 键，不动内容与版本（主题是展示层快照）。
func (m *Model) RefreshThemeForTheme(ctx context.Context, themeID string, themeJSON []byte) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,theme}', ?, true), updated_at = ? WHERE theme_id = ? AND deleted_at IS NULL",
		themeJSON, time.Now().UTC(), themeID,
	).Error
	return err
}

// RefreshStructureForTheme 把主题的页眉/页脚块绑定批量合入挂在该主题下
// 全部页面的 settings.structure（主题换绑全局块后调用，页面需重新构建生效）。
func (m *Model) RefreshStructureForTheme(ctx context.Context, themeID string, structureJSON []byte) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,structure}', ?, true), updated_at = ? WHERE theme_id = ? AND deleted_at IS NULL",
		structureJSON, time.Now().UTC(), themeID,
	).Error
	return err
}

// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用）。
func (m *Model) MarkStaleForTheme(ctx context.Context, themeID string) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET stale = true, updated_at = ? WHERE theme_id = ? AND deleted_at IS NULL",
		time.Now().UTC(), themeID,
	).Error
	return err
}

// MarkStaleForI18n 把全部未删除页面标记为待重建（界面文案词条变更后调用）。
//
// 文案词条（sys_i18n）参与构建：组件固定文案由构建期取词注入 HTML 字节
// （docs/06-D §10）。词条改动后所有页面产物都可能过期，故整站标记 stale；
// 触发源为后台 i18n CRUD（决策 D7，尚未实现）或运维脚本，内核只提供能力。
// 全表更新，与 MarkStaleForTheme 同一模式（stale=true 幂等）。
func (m *Model) MarkStaleForI18n(ctx context.Context) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET stale = true, updated_at = ? WHERE deleted_at IS NULL",
		time.Now().UTC(),
	).Error
	return err
}

// blockRefMatchCond 块引用匹配条件（JSONB 路径查询）。
//
// 语义：draft_document 中任意深度出现键 blockId 且值为字符串 <blockID> 的节点
// （core.globalref 的 props.blockId 是唯一来源，但该节点可嵌在 root 树任意
// 容器层级，故用递归通配 $.** 收集全部 blockId 值，再判数组包含）。
// 与旧写法 draft_document::text LIKE '%"blockId": "<blockID>"%' 结果集完全一致：
// 旧写法把 JSONB 序列化成 text 后子串匹配，而字符串值内部的引号在 JSONB 文本
// 输出中已转义为 \"，两者都不会误命中字符串字面量。
//
// 为什么不是 @? '$.**.blockId ? (@ == "x")'：jsonb_path_ops 无法索引递归
// 通配，EXPLAIN 下退化为索引内全扫（2 万行实测：索引扫描 18182 行后丢弃，
// 比 seq scan 更慢）。改成「值集合 + 数组包含」后表达式可被 GIN 精确索引。
//
// 本表达式与迁移 068 的 idx_pages_blockref 表达式一致（PG 按解析后的表达式树
// 比较，空白无关）；同一查询的 structure 分支（headerBlockId / footerBlockId）
// 另有 idx_pages_structure_header / _footer 两个 btree 表达式索引，三个 OR 分支
// 由 planner 用 BitmapOr 合并（2 万行实测 0.27ms，旧写法全表扫 55ms）。
// 改动本表达式必须同步迁移 068，否则索引静默失效
// （public/test/page/unit 有等价性与 EXPLAIN 断言守住）。
const blockRefMatchCond = `jsonb_path_query_array(draft_document, '$.**.blockId') @> jsonb_build_array(?::text)`

// CountBlockReference 统计引用该块的未删除页面数（与 MarkStaleForBlock 同一匹配条件）：
// core.globalref 节点（blockId）或 settings.structure 页眉/页脚自选绑定。
// 供 block 模块删除/切换 global→template 前的引用拦截（docs/02-D §9）。
func (m *Model) CountBlockReference(ctx context.Context, blockID string) (count int64, err error) {
	err = m.DB(ctx).
		Where("deleted_at IS NULL AND ("+
			blockRefMatchCond+
			" OR draft_document->'settings'->'structure'->>'headerBlockId' = ?"+
			" OR draft_document->'settings'->'structure'->>'footerBlockId' = ?)",
			blockID, blockID, blockID,
		).Count(&count).Error
	return count, err
}

// MarkStaleForBlock 把文档中经 core.globalref 引用（draft_document 树内
// "blockId": "<blockID>" 节点）或 settings.structure 页眉/页脚自选绑定
// （headerBlockId/footerBlockId，页面级覆盖，非主题默认）该块的页面标记为待重建。
// 与 MarkStaleForTheme 可能重叠命中同一页面，stale=true 幂等，无妨。
func (m *Model) MarkStaleForBlock(ctx context.Context, blockID string) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET stale = true, updated_at = ? WHERE deleted_at IS NULL AND ("+
			blockRefMatchCond+
			" OR draft_document->'settings'->'structure'->>'headerBlockId' = ?"+
			" OR draft_document->'settings'->'structure'->>'footerBlockId' = ?)",
		time.Now().UTC(), blockID, blockID, blockID,
	).Error
	return err
}

// AttachThemeToUnassigned 把工程内尚未挂主题的页面挂到指定主题。
// 工程首个主题创建时回填历史页面（迁移 020 的运行时兜底）。
func (m *Model) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET theme_id = ?, updated_at = ? WHERE project_id = ? AND theme_id IS NULL AND deleted_at IS NULL",
		themeID, time.Now().UTC(), projectID,
	).Error
	return err
}

// ReattachProjectPagesToTheme 把工程内全部页面（含已挂其他主题的）转挂到指定主题。
// 切换激活主题时调用，是「整站换皮」的前置：只有转挂后批量刷新（Refresh*/MarkStale*）
// 才能以该主题为键命中整站页面。不改 draft_document 内容，也不 bump 版本。
func (m *Model) ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET theme_id = ?, updated_at = ? WHERE project_id = ? AND deleted_at IS NULL",
		themeID, time.Now().UTC(), projectID,
	).Error
	return err
}

// GetByID 按 ID 查询未删除的 Page。
func (m *Model) GetByID(ctx context.Context, id string) (e *PageEntity, err error) {
	e = &PageEntity{}
	if err = m.DB(ctx).Where("id = ? AND deleted_at IS NULL", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListRevisions 按版本倒序读取修订快照。
func (m *Model) ListRevisions(ctx context.Context, pageID string) (list []RevisionEntity, err error) {
	err = m.RevisionDB(ctx).Where("page_id = ?", pageID).Order("version DESC").Find(&list).Error
	return list, err
}

// CreateWithRevision 原子创建 Page 与初始 Revision。
// 路径占用（page_routes 的 reserved 行）由 service 层经 publication contract
// 的 ReservePath 处理——page_routes 单一所有归 publication，page model 不碰该表。
func (m *Model) CreateWithRevision(ctx context.Context, page *PageEntity, revision *RevisionEntity) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(page).Error; err != nil {
			return err
		}
		return tx.Create(revision).Error
	})
}

// SaveDraftWithRevision 使用乐观锁原子保存草稿与修订。
// 改路径时的 reserved 占用迁移由 service 层经 publication contract 的
// RenameReserved 处理——page model 不再碰 page_routes。
func (m *Model) SaveDraftWithRevision(
	ctx context.Context,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, expectedVersion).
			Updates(map[string]any{
				"draft_path":     path,
				"draft_document": document,
				"draft_version":  nextVersion,
				"stale":          true,
				"updated_at":     updatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		return tx.Create(revision).Error
	})
}

// MarkPublished 回写活跃产物指针与发布元数据（发布/回滚共用）。
func (m *Model) MarkPublished(ctx context.Context, pageID, path, artifactID string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{
			"active_artifact_id": artifactID,
			"active_path":        path,
			"published_at":       at,
			"stale":              false,
			"updated_at":         at,
		}).Error
}

// MoveDraftPath 发布改 URL 后同步草稿路径（逻辑路径，不含语言前缀）。
// 激活路径不再在此处写：它按语言存放在 page_publications，
// 由 MovePublicationPath 单独同步（多语言 P3，docs/06-D §15.5 第 2 条）。
func (m *Model) MoveDraftPath(ctx context.Context, pageID, newPath string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"draft_path": newPath, "updated_at": at}).Error
}

// SoftDelete 软删 Page（deleted_at 置时间，审计留痕）；页面不存在或已软删
// 返回 gorm.ErrRecordNotFound。路径占用清理由 service 层经 publication contract
// 的 DeleteRoutesByPage 处理——page model 不再碰 page_routes。
// 同时清理 page_publications（同聚合原子组合）：软删后残留的激活记录
// 会让「同路径新建页面」读到幽灵激活状态。
func (m *Model) SoftDelete(ctx context.Context, pageID string, at time.Time) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL", pageID).
			Update("deleted_at", at)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if derr := tx.Model(&PublicationEntity{}).Where("page_id = ?", pageID).
			Delete(&PublicationEntity{}).Error; derr != nil {
			return derr
		}
		return tx.Model(&StagingEntity{}).Where("page_id = ?", pageID).
			Delete(&StagingEntity{}).Error
	})
}

// DraftPathValue 返回草稿访问路径（空安全）。
func (e *PageEntity) DraftPathValue() string {
	if e == nil {
		return ""
	}
	return e.DraftPath
}

// ActivePathValue 返回当前线上路径（未发布为空）。
func (e *PageEntity) ActivePathValue() string {
	if e == nil || e.ActivePath == nil {
		return ""
	}
	return *e.ActivePath
}

// DraftDocumentFor 优先返回产物冻结源文档，回退到当前草稿。
func (e *PageEntity) DraftDocumentFor(source json.RawMessage) json.RawMessage {
	if len(source) > 0 {
		return source
	}
	return e.DraftDocument
}
