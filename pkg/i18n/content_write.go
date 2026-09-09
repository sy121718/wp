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
// 幂等：主键 (source_hash, context, lang)，写入用 ON CONFLICT DO UPDATE，
// 同一条重复保存只更新不新增（updated_at 推进，ContentRevision 随之变化）。

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

// ContentWriteItem 一条待写入译文（主键 = source_hash + context + lang）。
type ContentWriteItem struct {
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
type contentTranslationEntity struct {
	SourceHash string    `gorm:"column:source_hash;primaryKey"`
	Context    string    `gorm:"column:context;primaryKey"`
	Lang       string    `gorm:"column:lang;primaryKey"`
	SourceText string    `gorm:"column:source_text"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

// TableName 绑定 sys_translation（066 迁移）。
func (contentTranslationEntity) TableName() string { return "sys_translation" }

// contentTranslationDetailQuery 工作台读取形态：在 P5a 的 (source_hash, lang) 索引上
// 多取 engine 与 updated_at 两列（P5a 的构建期查询形态不动，只用于取译文文本）。
const contentTranslationDetailQuery = `SELECT source_hash, context, target_text, engine, updated_at
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2`

// contentTranslationDetailRow 明细查询投影。
type contentTranslationDetailRow struct {
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
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

// LoadTargets 复用 P5a 读路径按 (hashes, lang) 批量取译文文本。
func (w *ContentWriter) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, lang, hashes)
}

// LoadDetails 按 (hashes, lang) 批量取译文明细（含 engine / updated_at）。
//
// 与 LoadTargets 的差别只有投影多两列，查询条件与索引完全相同；
// 供工作台渲染来源徽章、以及「保存是否真的改变了产物」的判定。
func (w *ContentWriter) LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	out := make(map[string]ContentTargetInfo)
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	lang = strings.TrimSpace(lang)
	if lang == "" || len(hashes) == 0 {
		return out, nil
	}
	var rows []contentTranslationDetailRow
	if err := w.db.WithContext(ctx).Raw(contentTranslationDetailQuery, hashes, lang).Scan(&rows).Error; err != nil {
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

// Upsert 批量写入译文（ON CONFLICT (source_hash, context, lang) DO UPDATE）。
//
// 全部条目先校验后写入：任一条不合法 → 整体拒绝（不写半批），
// 避免工作台一次提交里「部分成功」造成难以解释的中间态。
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
		Columns:   []clause.Column{{Name: "source_hash"}, {Name: "context"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_text", "target_text", "engine", "updated_at"}),
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
	return contentTranslationEntity{
		SourceHash: item.SourceHash, Context: contextName, Lang: lang,
		SourceText: item.SourceText, TargetText: target, Engine: engine, UpdatedAt: now,
	}, nil
}
