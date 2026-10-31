package pagemodel

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

var (
	ErrDraftVersionConflict = errors.New("page 草稿版本冲突")

	ErrProjectRequired = errors.New("page: 需要显式工程作用域")
)

const (
	tableNamePages         = "pages"
	tableNamePageRevisions = "page_revisions"
)

type PageEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`

	ThemeID           *string         `gorm:"column:theme_id"`
	Kind              string          `gorm:"column:kind;not null"`
	ContentTargetType string          `gorm:"column:content_target_type;not null"`
	ContentTargetID   *string         `gorm:"column:content_target_id"`
	DraftPath         string          `gorm:"column:draft_path;not null"`
	ActivePath        *string         `gorm:"column:active_path"`
	DraftDocument     json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion      int64           `gorm:"column:draft_version;not null"`
	StagedArtifactID  *string         `gorm:"column:staged_artifact_id"`
	ActiveArtifactID  *string         `gorm:"column:active_artifact_id"`
	Stale             bool            `gorm:"column:stale;not null"`

	InputVersion int64 `gorm:"column:input_version;not null"`

	ExcludedLangs StringArray `gorm:"column:excluded_langs;type:text[]"`
	DeletedAt     *time.Time  `gorm:"column:deleted_at"`
	PublishedAt   *time.Time  `gorm:"column:published_at"`
	CreatedAt     time.Time   `gorm:"column:create_time;not null"`
	UpdatedAt     time.Time   `gorm:"column:update_time;not null"`
}

func (PageEntity) TableName() string { return tableNamePages }

type RevisionEntity struct {
	ID            string          `gorm:"column:id;primaryKey"`
	PageID        string          `gorm:"column:page_id;not null"`
	Version       int64           `gorm:"column:version;not null"`
	DraftPath     string          `gorm:"column:draft_path;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	SourceHash    string          `gorm:"column:source_hash;not null"`
	CreatedAt     time.Time       `gorm:"column:create_time;not null"`
}

func (RevisionEntity) TableName() string { return tableNamePageRevisions }

type Model struct {
	db *gorm.DB
}

func NewPageModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageEntity{})
}

func (m *Model) RevisionDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RevisionEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) ListAll(ctx context.Context, projectID, themeID string) (list []PageEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&PageEntity{}).Omit("draft_document").Where("deleted_at IS NULL AND project_id = ?", projectID)
		if themeID != "" {

			q = q.Where("theme_id = ? OR theme_id IS NULL", themeID)
		}
		return q.Order("update_time DESC, id DESC").Find(&list).Error
	})
	return list, err
}

type PageTitleRow struct {
	ID         string  `gorm:"column:id"`
	Kind       string  `gorm:"column:kind"`
	DraftPath  string  `gorm:"column:draft_path"`
	ActivePath *string `gorm:"column:active_path"`
	SEOTitle   string  `gorm:"column:seo_title"`
}

func (m *Model) ListPageTitles(ctx context.Context, projectID string) (rows []PageTitleRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "kind", "draft_path", "active_path",
				"COALESCE(draft_document->'settings'->'seo'->>'title', '') AS seo_title").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Order("update_time DESC, id DESC").
			Find(&rows).Error
	})
	return rows, err
}

func (m *Model) ListDraftDocuments(ctx context.Context, projectID string) (list []PageEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "project_id", "draft_path", "draft_document", "update_time").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Order("update_time DESC, id DESC").
			Find(&list).Error
	})
	return list, err
}

type ThemePageSnapshot struct {
	ID       string
	Override json.RawMessage
}

func (m *Model) ListThemePageSnapshots(ctx context.Context, projectID, themeID string) (rows []ThemePageSnapshot, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	type row struct {
		ID         string
		ThemeOverr json.RawMessage `gorm:"column:theme_override"`
	}
	var raw []row
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "draft_document #> '{settings,themeOverride}' AS theme_override").
			Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
			Find(&raw).Error
	}); err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, ThemePageSnapshot{ID: r.ID, Override: r.ThemeOverr})
	}
	return rows, nil
}

func (m *Model) UpdateThemeSnapshot(ctx context.Context, projectID, pageID string, themeJSON []byte) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
			UpdateColumns(mergeInputChange(map[string]any{
				"draft_document": gorm.Expr("jsonb_set(draft_document, '{settings,theme}', ?, true)", themeJSON),
			}, time.Now().UTC())).Error
	})
}

type ThemePageStructureSnapshot struct {
	ID        string
	Structure json.RawMessage
}

func (m *Model) ListThemePageStructureSnapshots(ctx context.Context, projectID, themeID string) (rows []ThemePageStructureSnapshot, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	type row struct {
		ID        string
		Structure json.RawMessage `gorm:"column:page_structure"`
	}
	var raw []row
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "draft_document #> '{settings,structure}' AS page_structure").
			Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
			Find(&raw).Error
	}); err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, ThemePageStructureSnapshot{ID: r.ID, Structure: r.Structure})
	}
	return rows, nil
}

func (m *Model) UpdateStructureSnapshot(ctx context.Context, projectID, pageID string, structureJSON []byte) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
			UpdateColumns(mergeInputChange(map[string]any{
				"draft_document": gorm.Expr("jsonb_set(draft_document, '{settings,structure}', ?, true)", structureJSON),
			}, time.Now().UTC())).Error
	})
}

func pageIDsOf(rows []PageEntity) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func (m *Model) MarkStaleForTheme(ctx context.Context, projectID, themeID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var rows []PageEntity
		if err := tx.Model(&rows).
			Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(time.Now().UTC())).Error; err != nil {
			return err
		}
		ids = pageIDsOf(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (m *Model) MarkStaleForI18n(ctx context.Context, projectID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var merr error
		ids, merr = m.markStaleForI18nIn(ctx, tx, projectID, time.Now().UTC())
		return merr
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (m *Model) markStaleForI18nIn(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (ids []string, err error) {
	var rows []PageEntity
	err = tx.WithContext(ctx).
		Model(&rows).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Updates(staleMarkUpdates(at)).Error
	if err != nil {
		return nil, err
	}
	return pageIDsOf(rows), nil
}

func draftSaveUpdates(path string, document json.RawMessage, nextVersion int64, updatedAt time.Time) map[string]any {
	return map[string]any{
		"draft_path":     path,
		"draft_document": document,
		"draft_version":  nextVersion,
		"stale":          true,
		"input_version":  gorm.Expr("input_version + 1"),
		"update_time":    updatedAt,
	}
}

func staleMarkUpdates(at time.Time) map[string]any {
	return map[string]any{
		"stale":         true,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}
}

func inputChangeUpdates(at time.Time) map[string]any {
	return map[string]any{
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}
}

func mergeInputChange(cols map[string]any, at time.Time) map[string]any {
	for k, v := range inputChangeUpdates(at) {
		cols[k] = v
	}
	return cols
}

func (m *Model) MarkStaleByIDs(ctx context.Context, projectID string, ids []string, at time.Time) (marked []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var rows []PageEntity

		if err := tx.Model(&rows).
			Where("project_id = ? AND deleted_at IS NULL AND id = ANY(string_to_array(?, ',')::uuid[])",
				projectID, strings.Join(ids, ",")).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			UpdateColumns(map[string]any{
				"stale":         true,
				"input_version": gorm.Expr("input_version + 1"),
			}).Error; err != nil {
			return err
		}
		marked = pageIDsOf(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return marked, nil
}

const blockRefMatchCond = `jsonb_path_query_array(draft_document, '$.**.blockId') @> jsonb_build_array(?::text)`

const blockStructureMatchCond = `(draft_document->'settings'->'structure'->>'headerBlockId' = ?::text
			OR draft_document->'settings'->'structure'->>'footerBlockId' = ?::text
			OR jsonb_path_query_array(draft_document, '$.settings.structure.slots.*') @> jsonb_build_array(?::text))`

func (m *Model) CountBlockReference(ctx context.Context, projectID, blockID string) (count int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND ("+
				blockRefMatchCond+" OR "+blockStructureMatchCond+")",
				projectID, blockID, blockID, blockID, blockID,
			).Count(&count).Error
	})
	return count, err
}

type BlockSourceRefRow struct {
	ID          string `gorm:"column:id"`
	Path        string `gorm:"column:draft_path"`
	InDocument  bool   `gorm:"column:in_document"`
	InStructure bool   `gorm:"column:in_structure"`
}

func (m *Model) ListBlockSourceRefs(ctx context.Context, projectID, blockID string) (rows []BlockSourceRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		return tx.Table(tableNamePages).
			Select("id::text AS id, draft_path, ("+blockRefMatchCond+") AS in_document, ("+blockStructureMatchCond+") AS in_structure",
				blockID, blockID, blockID, blockID).
			Where("project_id = ? AND deleted_at IS NULL AND (("+blockRefMatchCond+") OR ("+blockStructureMatchCond+"))",
				projectID, blockID, blockID, blockID, blockID).
			Order("draft_path ASC, id ASC").
			Find(&rows).Error
	})
	return rows, err
}

type BlockRevisionRefRow struct {
	PageID  string `gorm:"column:page_id"`
	Path    string `gorm:"column:draft_path"`
	Version int64  `gorm:"column:version"`
}

func (m *Model) ListBlockRevisionRefs(ctx context.Context, projectID, blockID string) (rows []BlockRevisionRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNamePages+" AS p").
			Select("r.page_id::text AS page_id, p.draft_path, r.version").
			Joins("JOIN "+tableNamePageRevisions+" r ON r.page_id = p.id").
			Where("p.project_id = ? AND p.deleted_at IS NULL", projectID).
			Where("jsonb_path_query_array(r.draft_document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Order("p.draft_path ASC, r.version ASC").
			Find(&rows).Error
	})
	return rows, err
}

func (m *Model) MarkStaleForBlock(ctx context.Context, projectID, blockID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var rows []PageEntity
		if err := tx.Model(&rows).
			Where("project_id = ? AND deleted_at IS NULL AND ("+blockRefMatchCond+" OR "+blockStructureMatchCond+")",
				projectID, blockID, blockID, blockID, blockID).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(time.Now().UTC())).Error; err != nil {
			return err
		}
		ids = pageIDsOf(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (m *Model) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) (err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND theme_id IS NULL AND deleted_at IS NULL", projectID).
			UpdateColumns(map[string]any{"theme_id": themeID, "update_time": time.Now().UTC()}).Error
	})
	return err
}

func (m *Model) ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) (err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			UpdateColumns(map[string]any{"theme_id": themeID, "update_time": time.Now().UTC()}).Error
	})
	return err
}

func (m *Model) GetByID(ctx context.Context, id, projectID string) (e *PageEntity, err error) {
	e = &PageEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) ListRevisions(ctx context.Context, projectID, pageID string) (list []RevisionEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
			return err
		}
		return tx.Model(&RevisionEntity{}).Where("page_id = ?", pageID).
			Order("version DESC").Find(&list).Error
	})
	return list, err
}

func (m *Model) PruneRevisions(ctx context.Context, projectID, pageID string, keep int) (int64, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if strings.TrimSpace(pageID) == "" || keep < 1 {
		return 0, nil
	}
	var deleted int64
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
			return err
		}
		var threshold int
		err := tx.Model(&RevisionEntity{}).Where("page_id = ?", pageID).
			Order("version DESC").Offset(keep-1).Limit(1).
			Pluck("version", &threshold).Error
		if err != nil || threshold <= 1 {

			return err
		}
		res := tx.Model(&RevisionEntity{}).Where("page_id = ? AND version < ?", pageID, threshold).
			Delete(&RevisionEntity{})
		deleted = res.RowsAffected
		return res.Error
	})
	return deleted, err
}

func (m *Model) DeleteStaleRevisions(ctx context.Context, projectID string, keep int, cutoff time.Time, limit int) (int64, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if keep < 1 || limit < 1 {
		return 0, nil
	}

	const q = `DELETE FROM page_revisions WHERE ctid IN (
		SELECT ctid FROM (
			SELECT ctid, row_number() OVER (PARTITION BY page_id ORDER BY version DESC) AS rn, create_time, page_id
			FROM page_revisions
		) t WHERE t.rn > ? AND t.create_time < ?
		  AND EXISTS (SELECT 1 FROM pages p WHERE p.id = t.page_id AND p.project_id = ?)
		LIMIT ?
	)`
	res := m.DB(ctx).Exec(q, keep, cutoff, projectID, limit)
	return res.RowsAffected, res.Error
}

func (m *Model) CreateWithRevision(ctx context.Context, page *PageEntity, revision *RevisionEntity) (err error) {

	return rls.InProjectScope(ctx, m.db, page.ProjectID, func(tx *gorm.DB) error {
		if err := tx.Create(page).Error; err != nil {
			return err
		}
		return tx.Create(revision).Error
	})
}

func (m *Model) SaveDraftWithRevision(
	ctx context.Context,
	projectID string,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, projectID, expectedVersion).
			Updates(draftSaveUpdates(path, document, nextVersion, updatedAt))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		return tx.Create(revision).Error
	})
}

func (m *Model) MoveDraftPath(ctx context.Context, projectID, pageID, newPath string, at time.Time) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
			Updates(mergeInputChange(map[string]any{"draft_path": newPath}, at)).Error
	})
}

func (m *Model) SoftDelete(ctx context.Context, projectID, pageID string, at time.Time) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return m.softDeleteTx(ctx, nil, "", pageID, at)
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		return m.softDeleteTx(ctx, tx, projectID, pageID, at)
	})
}

func (m *Model) softDeleteTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, at time.Time) (err error) {
	scope := func(tx *gorm.DB) error {
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
		if derr := tx.Model(&StagingEntity{}).Where("page_id = ?", pageID).
			Delete(&StagingEntity{}).Error; derr != nil {
			return derr
		}

		return tx.Model(&PublicationPlanEntity{}).Where("page_id = ?", pageID).
			Delete(&PublicationPlanEntity{}).Error
	}
	if tx == nil {

		return m.Transaction(ctx, scope)
	}
	if projectID != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
	}
	return scope(tx)
}

func (m *Model) requirePageOwned(ctx context.Context, tx *gorm.DB, projectID, pageID string) error {
	var n int64
	if err := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ?", pageID, projectID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (e *PageEntity) DraftPathValue() string {
	if e == nil {
		return ""
	}
	return e.DraftPath
}

func (e *PageEntity) ActivePathValue() string {
	if e == nil || e.ActivePath == nil {
		return ""
	}
	return *e.ActivePath
}

func (e *PageEntity) DraftDocumentFor(source json.RawMessage) json.RawMessage {
	if len(source) > 0 {
		return source
	}
	return e.DraftDocument
}
