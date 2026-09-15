package i18n

// content_write.go — sys_translation 的写入端口与语境解析（多语言 P5c，docs/06-D §7.8）。
//
// P5a 只提供读取路径（content_store.go）；翻译工作台（P5c）是第一个写入口：
// 后台手动填写译文 → 写入 sys_translation（engine = manual）→ 构建期按
// (source_hash, context, lang) 取用。本文件只新增写入与语境解析，
// 不改 P5a 的取词语义与查询层（loadContentTargets / contentTranslationQuery 保持原样）。
//
// 三条硬约束（DDL 挡不住的必须在本层挡）：
//  1. sha256(source_text) 必须等于 source_hash —— 066 迁移的 CHECK 只校验格式，
//     不校验一致性；写错会让「原文已改」的行仍被命中（译文错位到新原文上）。
//  2. target_text 去空白后非空 —— 表上有 CHECK (target_text <> '')，
//     且空译文在构建期等同未命中，写空串是纯粹的噪声行。
//  3. engine 只允许 manual / ai / po（与 066 的 CHECK 一致）。
//
// 幂等：唯一键 (project_id, source_hash, context, lang)（迁移 195 取代 066 的主键，
// NULLS NOT DISTINCT），写入用 ON CONFLICT DO UPDATE，同一条重复保存只更新不新增
//（update_time 推进，ContentRevision 随之变化）。
//
// 工程作用域（审计 I18N-009）：ContentWriteItem.ProjectID 为空 = 全局共享行
//（project_id IS NULL，066 既有行的语义）；非空 = 仅该工程使用。
// 工作台保存的是**本工程**的译文，所以传当前工程 id —— 同一个原文在不同工程
// 各写各的译法，互不覆盖；未写的工程读全局行（读取侧的回落规则见 content_store.go）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 译文来源（engine 列取值，docs/06-D §7.3）：仅用于筛选与审阅，不参与取值逻辑。
const (
	ContentEngineManual = "manual"
	ContentEngineAI     = "ai"
	ContentEnginePO     = "po"
)

// 写入层错误（调用方按 errors.Is 分类；文案可直接展示给编辑者）。
var (
	// ErrContentWriteUnavailable 写入器未绑定数据库句柄 / 数据库未初始化。
	ErrContentWriteUnavailable = errors.New("内容译文存储不可用")
	// ErrContentHashMismatch 原文与 source_hash 不一致（写入前必须重算校验）。
	ErrContentHashMismatch = errors.New("原文与 source_hash 不一致")
	// ErrContentTargetEmpty 译文去空白后为空。
	ErrContentTargetEmpty = errors.New("译文不能为空")
	// ErrContentEngineInvalid engine 不在 manual / ai / po 之内。
	ErrContentEngineInvalid = errors.New("译文来源非法")
	// ErrContentLangEmpty 目标语言为空。
	ErrContentLangEmpty = errors.New("目标语言不能为空")
	// ErrContentContextEmpty 语境为空。
	ErrContentContextEmpty = errors.New("语境不能为空")
	// ErrOrphanProjectEmpty 孤儿检出/清理未指定工程（没有参照系时拒绝执行）。
	ErrOrphanProjectEmpty = errors.New("孤儿清理必须指定工程")
)

// ParseContentContext 拆解语境 {类型}.{字段名} → (类型, 字段名, ok)。
//
// 字段名不含点号（docs/06-D §7.5 规则 4：字段名只允许字母/数字/下划线），
// 故按最后一个点拆分即可；任一段为空返回 ok=false。
func ParseContentContext(contextName string) (typ, field string, ok bool) {
	ctx := strings.TrimSpace(contextName)
	i := strings.LastIndex(ctx, ".")
	if i <= 0 || i == len(ctx)-1 {
		return "", "", false
	}
	typ, field = strings.TrimSpace(ctx[:i]), strings.TrimSpace(ctx[i+1:])
	if typ == "" || field == "" {
		return "", "", false
	}
	return typ, field, true
}

// ContentWriteItem 一条待写入译文（唯一键 = project_id + source_hash + context + lang）。
type ContentWriteItem struct {
	// ProjectID 工程作用域（审计 I18N-009）：空 = 全局共享行，非空 = 仅该工程使用。
	ProjectID string
	// SourceHash 原文指纹（调用方按 ContentHash(sourceText) 计算；写入时重算校验）。
	SourceHash string
	// Context 语境（组件类型.字段名），如 core.button.text。
	Context string
	// Lang 目标语言，如 en-US。
	Lang string
	// SourceText 原文（冗余存储，供工作台对照与 hash 校验）。
	SourceText string
	// TargetText 译文（去空白后必须非空）。
	TargetText string
	// Engine 译文来源；空 → ContentEngineManual。
	Engine string
}

// ContentTargetInfo 已存在的译文行（工作台徽章与「是否变化」判定用）。
type ContentTargetInfo struct {
	TargetText string
	Engine     string
	UpdatedAt  time.Time
}

// contentTranslationEntity sys_translation 的写入投影。
//
// 不标 primaryKey：迁移 195 起唯一性是 uq_sys_translation_scope_key
// （含可空列 project_id；PG 的主键列不允许 NULL，故它不是主键）。
// 写入走 Upsert 显式声明的 ON CONFLICT 列，不依赖模型层的主键推断。
type contentTranslationEntity struct {
	ProjectID  *string   `gorm:"column:project_id"`
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	Lang       string    `gorm:"column:lang"`
	SourceText string    `gorm:"column:source_text"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

// TableName 绑定 sys_translation（066 迁移）。
func (contentTranslationEntity) TableName() string { return "sys_translation" }

// contentTranslationDetailQuery 工作台读取形态（无工程上下文）：在 (source_hash, lang)
// 索引上多取 engine 与 update_time 两列，作用域规则与构建期一致（全局行优先；
// 同一 (hash, context) 只回一行，理由见 content_store.go 的同名说明）。
const contentTranslationDetailQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text, engine, update_time
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2
ORDER BY source_hash, context, (project_id IS NULL) DESC, update_time DESC`

// contentTranslationDetailProjectQuery 工作台读取形态（工程作用域，审计 I18N-009）：
// 与构建期同一条作用域规则（工程行优先、回落全局行），只是投影多两列。
// $3 = uuid（当前工程）。
const contentTranslationDetailProjectQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text, engine, update_time
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2 AND (project_id IS NULL OR project_id = $3::uuid)
ORDER BY source_hash, context, (project_id IS NOT NULL) DESC, update_time DESC`

// contentTranslationDetailRow 明细查询投影。
type contentTranslationDetailRow struct {
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

// ContentWriter sys_translation 写入器（工作台与未来的 AI 译文共用同一写入路径）。
type ContentWriter struct {
	db *gorm.DB
}

// NewContentWriter 用给定 gorm 句柄构造写入器（测试注入隔离 schema 用）。
func NewContentWriter(db *gorm.DB) *ContentWriter { return &ContentWriter{db: db} }

// NewContentWriterDefault 用默认库（database.GetDB()）构造写入器。
func NewContentWriterDefault() (*ContentWriter, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return &ContentWriter{db: db}, nil
}

// LoadTargets 复用 P5a 读路径按 (hashes, lang) 批量取译文文本（无工程上下文）。
func (w *ContentWriter) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, "", lang, hashes)
}

// LoadTargetsForProject 按 (工程, hashes, lang) 批量取译文文本（工程行优先、回落全局行）。
func (w *ContentWriter) LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, projectID, lang, hashes)
}

// LoadDetails 按 (hashes, lang) 批量取译文明细（含 engine / update_time）。
//
// 与 LoadTargets 的差别只有投影多两列，查询条件与索引完全相同；
// 供工作台渲染来源徽章、以及「保存是否真的改变了产物」的判定。
func (w *ContentWriter) LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	return w.LoadDetailsForProject(ctx, "", lang, hashes)
}

// LoadDetailsForProject 按 (工程, hashes, lang) 批量取译文明细（审计 I18N-009）。
//
// 作用域规则与构建期一致：工程行优先、未命中回落全局行。工作台据此展示
// 「本工程自己的译法」而不是其它工程的（未覆盖时才显示全局译文）。
func (w *ContentWriter) LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	out := make(map[string]ContentTargetInfo)
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	lang = strings.TrimSpace(lang)
	if lang == "" || len(hashes) == 0 {
		return out, nil
	}
	projectID = strings.TrimSpace(projectID)
	query, args := contentTranslationDetailQuery, []any{hashes, lang}
	if projectID != "" {
		query, args = contentTranslationDetailProjectQuery, []any{hashes, lang, projectID}
	}
	var rows []contentTranslationDetailRow
	if err := w.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.TargetText == "" {
			continue
		}
		out[ContentIndexKey(row.SourceHash, row.Context)] = ContentTargetInfo{
			TargetText: row.TargetText, Engine: row.Engine, UpdatedAt: row.UpdatedAt,
		}
	}
	return out, nil
}

// Upsert 批量写入译文
// （ON CONFLICT (project_id, source_hash, context, lang) DO UPDATE）。
//
// 全部条目先校验后写入：任一条不合法 → 整体拒绝（不写半批），
// 避免工作台一次提交里「部分成功」造成难以解释的中间态。
// 作用域由条目自身的 ProjectID 决定（空 = 全局共享行）；不同工程的同 key 译文
// 落在不同的唯一键上，因此各自独立、互不覆盖。
// 返回写入条数（含覆盖更新）。
func (w *ContentWriter) Upsert(ctx context.Context, items []ContentWriteItem) (written int, err error) {
	if w == nil || w.db == nil {
		return 0, ErrContentWriteUnavailable
	}
	if len(items) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	rows := make([]contentTranslationEntity, 0, len(items))
	for i := range items {
		row, verr := normalizeContentWriteItem(items[i], now)
		if verr != nil {
			return 0, verr
		}
		rows = append(rows, row)
	}
	if err = w.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "source_hash"}, {Name: "context"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_text", "target_text", "engine", "update_time"}),
	}).Create(&rows).Error; err != nil {
		return 0, err
	}
	return len(rows), nil
}

// normalizeContentWriteItem 校验并归一单条写入（返回可直接入库的行）。
func normalizeContentWriteItem(item ContentWriteItem, now time.Time) (row contentTranslationEntity, err error) {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" {
		return row, ErrContentLangEmpty
	}
	contextName := strings.TrimSpace(item.Context)
	if contextName == "" {
		return row, ErrContentContextEmpty
	}
	target := strings.TrimSpace(item.TargetText)
	if target == "" {
		return row, ErrContentTargetEmpty
	}
	// 原文指纹一致性：066 迁移只校验 hash 格式，一致性必须在此处挡。
	if item.SourceHash != ContentHash(item.SourceText) {
		return row, ErrContentHashMismatch
	}
	engine := strings.TrimSpace(item.Engine)
	if engine == "" {
		engine = ContentEngineManual
	}
	switch engine {
	case ContentEngineManual, ContentEngineAI, ContentEnginePO:
	default:
		return row, ErrContentEngineInvalid
	}
	// 工程作用域：空 = 全局共享行（project_id IS NULL），非空 = 仅该工程使用。
	var projectID *string
	if scoped := strings.TrimSpace(item.ProjectID); scoped != "" {
		projectID = &scoped
	}
	return contentTranslationEntity{
		ProjectID: projectID, SourceHash: item.SourceHash, Context: contextName, Lang: lang,
		SourceText: item.SourceText, TargetText: target, Engine: engine, UpdatedAt: now,
	}, nil
}
