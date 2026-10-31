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

func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func (m *Model) CreateWithRevisionTx(ctx context.Context, tx *gorm.DB, page *PageEntity, revision *RevisionEntity) error {
	if err := rls.ScopeTx(tx, page.ProjectID); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(page).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(revision).Error
}

func (m *Model) SaveDraftWithRevisionTx(
	ctx context.Context,
	tx *gorm.DB,
	projectID string,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, projectID, expectedVersion).
		Updates(draftSaveUpdates(path, document, nextVersion, updatedAt))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrDraftVersionConflict
	}
	return tx.WithContext(ctx).Create(revision).Error
}

func (m *Model) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	return m.markStaleForI18nIn(ctx, tx, projectID, at)
}

func (m *Model) DeletePublicationsByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PublicationEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationEntity{}).Error
}

func (m *Model) DeleteStagingsByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&StagingEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&StagingEntity{}).Error
}

func (m *Model) MoveDraftPathTx(ctx context.Context, tx *gorm.DB, projectID, pageID, newPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		Updates(mergeInputChange(map[string]any{"draft_path": newPath}, at)).Error
}

func (m *Model) MarkPublishedLangTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	row := &PublicationEntity{
		PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
		ReceiptID: rec.receiptIDColumn(),
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if cerr := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time", "receipt_id",
		}),
	}).Create(row).Error; cerr != nil {
		return cerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", rec.PageID).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		}).Error
}

func (m *Model) MarkStagedLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang, artifactID, artifactHash string, draftVersion int64, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	row := &StagingEntity{
		PageID: pageID, Lang: lang, ArtifactID: artifactID,
		ArtifactHash: artifactHash, DraftVersion: draftVersion, UpdatedAt: at,
	}
	if cerr := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"artifact_id", "artifact_hash", "draft_version", "update_time",
		}),
	}).Create(row).Error; cerr != nil {
		return cerr
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"staged_artifact_id": artifactID, "update_time": at}).Error
}

func (m *Model) ReplaceDependenciesTx(ctx context.Context, tx *gorm.DB, projectID, pageID, artifactID string, rows []DependencyEntity) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
		return err
	}
	if derr := tx.WithContext(ctx).Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
		return derr
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(rows, 200).Error
}

func (m *Model) SoftDeleteTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return m.softDeleteTx(ctx, tx, projectID, pageID, at)
}

func (m *Model) MarkStaleByDependencyTx(ctx context.Context, tx *gorm.DB, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	return markStaleByDependencyIn(ctx, tx, projectID, kind, key, at)
}

const depKindMenu = "menu"

const depKindNavigation = "navigation"

func menuDependencyKey(projectID, kind string) string {
	return "menu:" + projectID + ":" + kind
}

func navigationDependencyKey(navigationID string) string {
	return "navigation:" + navigationID
}

type PageActivationSnapshot struct {
	PageID string

	InputVersion int64

	ActiveArtifactID string
}

func (m *Model) LockPageEntityAdvisoryTx(ctx context.Context, tx *gorm.DB, pageID string) error {
	if tx == nil {
		return errors.New("page: 实体顾问锁必须在调用方事务内取")
	}
	key := strings.TrimSpace(pageID)
	if key == "" {
		return ErrProjectRequired
	}

	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
		"page:"+key).Error
}

func (m *Model) LockProjectEntityAdvisoryTx(ctx context.Context, tx *gorm.DB, projectID string) error {
	if tx == nil {
		return errors.New("page: 工程顾问锁必须在调用方事务内取")
	}
	key := strings.TrimSpace(projectID)
	if key == "" {
		return ErrProjectRequired
	}
	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
		"project:"+key).Error
}

func (m *Model) LockPageForActivationTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) (snap PageActivationSnapshot, err error) {
	if strings.TrimSpace(projectID) == "" {
		return snap, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return snap, err
	}
	var row PageEntity
	terr := tx.WithContext(ctx).Model(&PageEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		First(&row).Error
	if terr != nil {
		return snap, terr
	}
	snap.PageID = row.ID
	snap.InputVersion = row.InputVersion
	if row.ActiveArtifactID != nil {
		snap.ActiveArtifactID = *row.ActiveArtifactID
	}
	return snap, nil
}

func (m *Model) WritePageActivationTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord, expectInputVersion int64) (written bool, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return false, err
	}
	if err := m.upsertPublicationTx(ctx, tx, rec); err != nil {
		return false, err
	}
	res := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND input_version = ?",
			rec.PageID, projectID, expectInputVersion).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (m *Model) WritePageForwardCompletionTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord, expectInputVersion int64, fromArtifactID string) (written bool, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return false, err
	}

	cur, gerr := m.publicationForUpdateTx(ctx, tx, rec.PageID, rec.Lang)
	if gerr != nil {
		return false, gerr
	}
	curID := ""
	if cur != nil && cur.ArtifactID != nil {
		curID = *cur.ArtifactID
	}
	if curID != "" && curID != strings.TrimSpace(fromArtifactID) {

		return false, nil
	}
	if err := m.upsertPublicationTx(ctx, tx, rec); err != nil {
		return false, err
	}
	res := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL AND input_version = ?",
			rec.PageID, projectID, expectInputVersion).
		Updates(map[string]any{
			"active_artifact_id": rec.ArtifactID,
			"active_path":        rec.ActivePath,
			"published_at":       rec.PublishedAt,
			"stale":              false,
			"update_time":        rec.PublishedAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (m *Model) upsertPublicationTx(ctx context.Context, tx *gorm.DB, rec PublicationRecord) error {
	row := &PublicationEntity{
		PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
		ReceiptID: rec.receiptIDColumn(),
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}

	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time", "receipt_id",
		}),
	}).Create(row).Error
}

func (m *Model) publicationForUpdateTx(ctx context.Context, tx *gorm.DB, pageID, lang string) (*PublicationEntity, error) {
	var row PublicationEntity
	err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("page_id = ? AND lang = ?", pageID, lang).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) PublicationArtifactIDTx(ctx context.Context, tx *gorm.DB, pageID, lang string) (string, error) {
	var rows []struct {
		ArtifactID *string `gorm:"column:artifact_id"`
	}
	if err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Select("artifact_id").Where("page_id = ? AND lang = ?", pageID, lang).
		Limit(1).Find(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ArtifactID == nil {
		return "", nil
	}
	return *rows[0].ArtifactID, nil
}

func (m *Model) PublicationReceiptIDTx(ctx context.Context, tx *gorm.DB, pageID, lang string) (int64, error) {
	var rows []struct {
		ReceiptID *int64 `gorm:"column:receipt_id"`
	}
	if err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Select("receipt_id").Where("page_id = ? AND lang = ?", pageID, lang).
		Limit(1).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 || rows[0].ReceiptID == nil {
		return 0, nil
	}
	return *rows[0].ReceiptID, nil
}

func (m *Model) PublicationActivePathsTx(ctx context.Context, tx *gorm.DB, pageID string) ([]string, error) {
	var rows []struct {
		ActivePath string `gorm:"column:active_path"`
	}
	if err := tx.WithContext(ctx).Model(&PublicationEntity{}).
		Select("active_path").Where("page_id = ?", pageID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.ActivePath) == "" {
			continue
		}
		out = append(out, r.ActivePath)
	}
	return out, nil
}

func (m *Model) PublicationReceiptIDTxAny(ctx context.Context, projectID, pageID, lang string) (int64, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	var id int64
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		got, gerr := m.PublicationReceiptIDTx(ctx, tx, pageID, lang)
		if gerr != nil {
			return gerr
		}
		id = got
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (m *Model) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if strings.TrimSpace(kind) == "" {
		return 0, nil
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	ids, err := markStaleByDependencyIn(ctx, tx, projectID, depKindMenu, menuDependencyKey(projectID, kind), at)
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func (m *Model) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (n int64, err error) {
	navigationID = strings.TrimSpace(navigationID)
	if navigationID == "" {
		return 0, nil
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}

	ids, err := markStaleByDependencyIn(ctx, tx, projectID, depKindNavigation, navigationDependencyKey(navigationID), time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}
