package i18n

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/database"
	"go_wp/pkg/rls"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ContentEngineManual = "manual"
	ContentEngineAI     = "ai"
	ContentEnginePO     = "po"
)

var (
	ErrContentWriteUnavailable = errors.New("内容译文存储不可用")

	ErrContentHashMismatch = errors.New("原文与 source_hash 不一致")

	ErrContentTargetEmpty = errors.New("译文不能为空")

	ErrContentEngineInvalid = errors.New("译文来源非法")

	ErrContentLangEmpty = errors.New("目标语言不能为空")

	ErrContentContextEmpty = errors.New("语境不能为空")

	ErrOrphanProjectEmpty = errors.New("孤儿清理必须指定工程")

	ErrContentTxRequired = errors.New("译文写入需要调用方事务")

	ErrContentProjectMixed = errors.New("批量写入只能属于同一工程")

	ErrContentProjectRequired = errors.New("译文写入锁必须指定合法工程")
)

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

type ContentWriteItem struct {
	ProjectID string

	SourceHash string

	Context string

	Lang string

	SourceText string

	TargetText string

	Engine string
}

type ContentTargetInfo struct {
	TargetText string
	Engine     string
	UpdatedAt  time.Time
}

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

func (contentTranslationEntity) TableName() string { return "sys_translation" }

const contentTranslationDetailQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text, engine, update_time
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2
ORDER BY source_hash, context, (project_id IS NULL) DESC, update_time DESC`

const contentTranslationDetailProjectQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text, engine, update_time
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2 AND (project_id IS NULL OR project_id = $3::uuid)
ORDER BY source_hash, context, (project_id IS NOT NULL) DESC, update_time DESC`

type contentTranslationDetailRow struct {
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

type ContentWriter struct {
	db *gorm.DB
}

func NewContentWriter(db *gorm.DB) *ContentWriter { return &ContentWriter{db: db} }

func NewContentWriterDefault() (*ContentWriter, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return &ContentWriter{db: db}, nil
}

func (w *ContentWriter) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, "", lang, hashes)
}

func (w *ContentWriter) LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, projectID, lang, hashes)
}

func (w *ContentWriter) LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	return w.LoadDetailsForProject(ctx, "", lang, hashes)
}

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
	var rows []contentTranslationDetailRow
	if projectID == "" {
		if err := w.db.WithContext(ctx).Raw(
			contentTranslationDetailQuery, hashes, lang).Scan(&rows).Error; err != nil {
			return nil, err
		}
	} else {

		if err := rls.InProjectScope(ctx, w.db, projectID, func(tx *gorm.DB) error {
			return tx.Raw(contentTranslationDetailProjectQuery, hashes, lang, projectID).Scan(&rows).Error
		}); err != nil {
			return nil, err
		}
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

type contentReadScope struct {
	projectID string
	lang      string
	hashes    []string
}

func (s contentReadScope) empty() bool { return s.lang == "" || len(s.hashes) == 0 }

func normalizeContentReadScope(projectID, lang string, hashes []string) contentReadScope {
	return contentReadScope{projectID: strings.TrimSpace(projectID), lang: strings.TrimSpace(lang), hashes: hashes}
}

func contentTargetsFromRows(rows []contentTranslationRow) map[string]string {
	targets := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.TargetText == "" {
			continue
		}
		targets[ContentIndexKey(row.SourceHash, row.Context)] = row.TargetText
	}
	return targets
}

func contentDetailsFromRows(rows []contentTranslationDetailRow) map[string]ContentTargetInfo {
	out := make(map[string]ContentTargetInfo, len(rows))
	for _, row := range rows {
		if row.TargetText == "" {
			continue
		}
		out[ContentIndexKey(row.SourceHash, row.Context)] = ContentTargetInfo{
			TargetText: row.TargetText, Engine: row.Engine, UpdatedAt: row.UpdatedAt,
		}
	}
	return out
}

func (w *ContentWriter) LoadTargetsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string, hashes []string) (map[string]string, error) {

	if w == nil {
		return nil, ErrContentWriteUnavailable
	}
	if err := requireTx(tx); err != nil {
		return nil, err
	}
	scope := normalizeContentReadScope(projectID, lang, hashes)
	if scope.empty() {
		return make(map[string]string), nil
	}
	rows, err := loadContentTranslationRowsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	return contentTargetsFromRows(rows), nil
}

func (w *ContentWriter) LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	if w == nil {
		return nil, ErrContentWriteUnavailable
	}
	if err := requireTx(tx); err != nil {
		return nil, err
	}
	scope := normalizeContentReadScope(projectID, lang, hashes)
	if scope.empty() {
		return make(map[string]ContentTargetInfo), nil
	}
	var rows []contentTranslationDetailRow
	var err error
	if scope.projectID == "" {
		err = tx.WithContext(ctx).Raw(contentTranslationDetailQuery, scope.hashes, scope.lang).Scan(&rows).Error
	} else {
		err = tx.WithContext(ctx).Raw(
			contentTranslationDetailProjectQuery, scope.hashes, scope.lang, scope.projectID).Scan(&rows).Error
	}
	if err != nil {
		return nil, err
	}
	return contentDetailsFromRows(rows), nil
}

func loadContentTranslationRowsTx(ctx context.Context, tx *gorm.DB, scope contentReadScope) ([]contentTranslationRow, error) {
	var rows []contentTranslationRow
	var err error
	if scope.projectID == "" {
		err = tx.WithContext(ctx).Raw(contentTranslationQuery, scope.hashes, scope.lang).Scan(&rows).Error
	} else {
		err = tx.WithContext(ctx).Raw(
			contentTranslationProjectQuery, scope.hashes, scope.lang, scope.projectID).Scan(&rows).Error
	}
	if err != nil {
		return nil, err
	}
	return rows, nil
}

const contentWriteLockSQL = "SELECT pg_advisory_xact_lock(hashtextextended($1, $2))"

const contentWriteLockKeyPrefix = "i18n.content:"

func LockProjectContentWrite(ctx context.Context, tx *gorm.DB, projectID string) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ErrContentProjectRequired
	}
	parsed, err := uuid.Parse(projectID)
	if err != nil {
		return ErrContentProjectRequired
	}
	key := contentWriteLockKeyPrefix + parsed.String()
	return tx.WithContext(ctx).Exec(contentWriteLockSQL, key, 0).Error
}

func (w *ContentWriter) Upsert(ctx context.Context, items []ContentWriteItem) (written int, err error) {
	if w == nil || w.db == nil {
		return 0, ErrContentWriteUnavailable
	}
	if len(items) == 0 {
		return 0, nil
	}
	err = w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var werr error
		written, werr = w.upsertContentGroupsTx(ctx, tx, items)
		return werr
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

func (w *ContentWriter) UpsertTx(ctx context.Context, tx *gorm.DB, items []ContentWriteItem) (written int, err error) {

	if w == nil {
		return 0, ErrContentWriteUnavailable
	}
	if err := requireTx(tx); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	group, err := normalizeContentWriteGroup(items, now)
	if err != nil {
		return 0, err
	}
	if werr := upsertContentTranslationsTx(tx, group.rows); werr != nil {
		return 0, werr
	}
	return len(group.rows), nil
}

func (w *ContentWriter) upsertContentGroupsTx(_ context.Context, tx *gorm.DB, items []ContentWriteItem) (written int, err error) {
	now := time.Now().UTC()
	rows := make([]contentTranslationEntity, 0, len(items))
	for i := range items {
		row, verr := normalizeContentWriteItem(items[i], now)
		if verr != nil {
			return 0, verr
		}
		rows = append(rows, row)
	}
	groups := make([]contentWriteGroup, 0, 2)
	groupIndex := make(map[string]int, 2)
	for i := range rows {
		key := ""
		if rows[i].ProjectID != nil {
			key = *rows[i].ProjectID
		}
		gi, ok := groupIndex[key]
		if !ok {
			groups = append(groups, contentWriteGroup{projectID: key})
			gi = len(groups) - 1
			groupIndex[key] = gi
		}
		groups[gi].rows = append(groups[gi].rows, rows[i])
	}
	for gi := range groups {
		g := &groups[gi]
		if g.projectID != "" {
			if serr := rls.ScopeTx(tx, g.projectID); serr != nil {
				return 0, serr
			}
		}
		if werr := upsertContentTranslationsTx(tx, g.rows); werr != nil {
			return 0, werr
		}
	}
	return len(rows), nil
}

func normalizeContentWriteGroup(items []ContentWriteItem, now time.Time) (contentWriteGroup, error) {
	rows := make([]contentTranslationEntity, 0, len(items))
	groupID := ""
	for i := range items {
		row, err := normalizeContentWriteItem(items[i], now)
		if err != nil {
			return contentWriteGroup{}, err
		}
		id := ""
		if row.ProjectID != nil {
			id = *row.ProjectID
		}
		if i == 0 {
			groupID = id
		} else if id != groupID {
			return contentWriteGroup{}, ErrContentProjectMixed
		}
		rows = append(rows, row)
	}
	return contentWriteGroup{projectID: groupID, rows: rows}, nil
}

func requireTx(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil || tx.Statement.ConnPool == nil {
		return ErrContentTxRequired
	}
	if err := tx.Error; err != nil {
		return err
	}
	switch conn := tx.Statement.ConnPool.(type) {
	case *sql.Tx:
		if conn == nil {
			return ErrContentTxRequired
		}
		return nil
	case *gorm.PreparedStmtTX:

		if conn == nil || conn.Tx == nil {
			return ErrContentTxRequired
		}
		return nil
	default:
		return ErrContentTxRequired
	}
}

type contentWriteGroup struct {
	projectID string
	rows      []contentTranslationEntity
}

func upsertContentTranslationsTx(tx *gorm.DB, rows []contentTranslationEntity) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "source_hash"}, {Name: "context"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_text", "target_text", "engine", "update_time"}),
	}).Create(&rows).Error
}

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

	var projectID *string
	if scoped := strings.TrimSpace(item.ProjectID); scoped != "" {
		projectID = &scoped
	}
	return contentTranslationEntity{
		ProjectID: projectID, SourceHash: item.SourceHash, Context: contextName, Lang: lang,
		SourceText: item.SourceText, TargetText: target, Engine: engine, UpdatedAt: now,
	}, nil
}
