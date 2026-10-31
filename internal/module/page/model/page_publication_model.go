package pagemodel

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/rls"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNamePagePublications = "page_publications"

type PublicationEntity struct {
	PageID       string    `gorm:"column:page_id;primaryKey"`
	Lang         string    `gorm:"column:lang;primaryKey"`
	ActivePath   string    `gorm:"column:active_path;not null"`
	ArtifactID   *string   `gorm:"column:artifact_id"`
	ArtifactHash string    `gorm:"column:artifact_hash;not null"`
	PublishedAt  time.Time `gorm:"column:published_at;not null"`
	UpdatedAt    time.Time `gorm:"column:update_time;not null"`

	ReceiptID *int64 `gorm:"column:receipt_id"`
}

func (PublicationEntity) TableName() string { return tableNamePagePublications }

type PublicationRecord struct {
	PageID       string
	Lang         string
	ActivePath   string
	ArtifactID   string
	ArtifactHash string
	PublishedAt  time.Time

	ReceiptID int64
}

func (r PublicationRecord) receiptIDColumn() *int64 {
	if r.ReceiptID <= 0 {
		return nil
	}
	id := r.ReceiptID
	return &id
}

func (m *Model) ListStalePageIDs(ctx context.Context, projectID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	var rows []artifactIDRow
	if err = m.db.WithContext(ctx).Model(&PageEntity{}).
		Select("id::text AS id").
		Where("project_id = ? AND deleted_at IS NULL AND stale = true", projectID).
		Order("update_time ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return dedupeStrings(artifactIDsOf(rows)), nil
}

func (m *Model) PublicationDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationEntity{})
}

func (m *Model) GetPublication(ctx context.Context, pageID, lang string) (e *PublicationEntity, err error) {
	e = &PublicationEntity{}
	if err = m.PublicationDB(ctx).Where("page_id = ? AND lang = ?", pageID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) ListPublicationsByPages(ctx context.Context, pageIDs []string, lang string) (list []PublicationEntity, err error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	q := m.PublicationDB(ctx).Where("page_id IN ?", pageIDs)
	if lang != "" {
		q = q.Where("lang = ?", lang)
	}
	err = q.Find(&list).Error
	return list, err
}

func (m *Model) ListPublications(ctx context.Context, pageID string) (list []PublicationEntity, err error) {
	err = m.PublicationDB(ctx).Where("page_id = ?", pageID).Order("lang ASC").Find(&list).Error
	return list, err
}

func (m *Model) ListPublicationsInProject(ctx context.Context, projectID, pageID string) (list []PublicationEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if strings.TrimSpace(pageID) == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PublicationEntity{}).Where("page_id = ?", pageID).
			Order("lang ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) MarkPublishedLang(ctx context.Context, rec PublicationRecord) (err error) {
	if rec.PageID == "" || rec.Lang == "" || rec.ActivePath == "" {
		return errors.New("激活记录缺少 page_id/lang/active_path")
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		row := &PublicationEntity{
			PageID: rec.PageID, Lang: rec.Lang, ActivePath: rec.ActivePath,
			ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
			ReceiptID: rec.receiptIDColumn(),
		}
		if rec.ArtifactID != "" {
			id := rec.ArtifactID
			row.ArtifactID = &id
		}
		if cerr := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"active_path", "artifact_id", "artifact_hash", "published_at", "update_time", "receipt_id",
			}),
		}).Create(row).Error; cerr != nil {
			return cerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", rec.PageID).
			Updates(map[string]any{
				"active_artifact_id": rec.ArtifactID,
				"active_path":        rec.ActivePath,
				"published_at":       rec.PublishedAt,
				"stale":              false,
				"update_time":        rec.PublishedAt,
			}).Error
	})
}

func (m *Model) MovePublicationPath(ctx context.Context, pageID, lang, activePath string, at time.Time) (err error) {
	if pageID == "" || lang == "" || activePath == "" {
		return errors.New("迁移激活路径缺少 page_id/lang/active_path")
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := tx.Model(&PublicationEntity{}).
			Where("page_id = ? AND lang = ?", pageID, lang).
			Updates(map[string]any{"active_path": activePath, "update_time": at}).Error; uerr != nil {
			return uerr
		}
		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
			Updates(map[string]any{"active_path": activePath, "update_time": at}).Error
	})
}

func (m *Model) DeletePublications(ctx context.Context, pageID string) (err error) {
	return m.PublicationDB(ctx).Where("page_id = ?", pageID).Delete(&PublicationEntity{}).Error
}

const tableNamePageStagings = "page_stagings"

type StagingEntity struct {
	PageID       string    `gorm:"column:page_id;primaryKey"`
	Lang         string    `gorm:"column:lang;primaryKey"`
	ArtifactID   string    `gorm:"column:artifact_id;not null"`
	ArtifactHash string    `gorm:"column:artifact_hash;not null"`
	DraftVersion int64     `gorm:"column:draft_version;not null"`
	UpdatedAt    time.Time `gorm:"column:update_time;not null"`
}

func (StagingEntity) TableName() string { return tableNamePageStagings }

func (m *Model) GetStaging(ctx context.Context, pageID, lang string) (e *StagingEntity, err error) {
	e = &StagingEntity{}
	if err = m.db.WithContext(ctx).Model(&StagingEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) MarkStagedLang(ctx context.Context, pageID, lang, artifactID, artifactHash string, draftVersion int64, at time.Time) (err error) {
	if pageID == "" || lang == "" || artifactID == "" {
		return errors.New("暂存记录缺少 page_id/lang/artifact_id")
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		row := &StagingEntity{
			PageID: pageID, Lang: lang, ArtifactID: artifactID,
			ArtifactHash: artifactHash, DraftVersion: draftVersion, UpdatedAt: at,
		}
		if cerr := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"artifact_id", "artifact_hash", "draft_version", "update_time",
			}),
		}).Create(row).Error; cerr != nil {
			return cerr
		}

		return tx.Model(&PageEntity{}).Where("id = ? AND deleted_at IS NULL", pageID).
			Updates(map[string]any{"staged_artifact_id": artifactID, "update_time": at}).Error
	})
}

func (m *Model) DeletePublicationsByLang(ctx context.Context, pageID, lang string) (err error) {
	return m.PublicationDB(ctx).Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationEntity{}).Error
}

func (m *Model) DeleteStagingsByLang(ctx context.Context, pageID, lang string) (err error) {
	return m.db.WithContext(ctx).Model(&StagingEntity{}).Where("page_id = ? AND lang = ?", pageID, lang).
		Delete(&StagingEntity{}).Error
}
func (m *Model) DeleteStagings(ctx context.Context, pageID string) (err error) {
	return m.db.WithContext(ctx).Model(&StagingEntity{}).Where("page_id = ?", pageID).
		Delete(&StagingEntity{}).Error
}

func (m *Model) ListCurrentArtifactIDs(ctx context.Context) (ids []string, err error) {

	var pubs, stagings []artifactIDRow
	if err = m.db.WithContext(ctx).Table(tableNamePagePublications).
		Select("artifact_id::text AS id").
		Where("artifact_id IS NOT NULL").
		Find(&pubs).Error; err != nil {
		return nil, err
	}
	if err = m.db.WithContext(ctx).Table(tableNamePageStagings).
		Select("artifact_id::text AS id").
		Find(&stagings).Error; err != nil {
		return nil, err
	}
	return dedupeStrings(artifactIDsOf(pubs, stagings)), nil
}

func (m *Model) ListProtectedArtifactIDs(ctx context.Context) (ids []string, err error) {

	var active, staged, pubs, stagings []artifactIDRow
	if err = m.db.WithContext(ctx).Model(&PageEntity{}).
		Select("active_artifact_id::text AS id").
		Where("deleted_at IS NULL AND active_artifact_id IS NOT NULL").
		Find(&active).Error; err != nil {
		return nil, err
	}
	if err = m.db.WithContext(ctx).Model(&PageEntity{}).
		Select("staged_artifact_id::text AS id").
		Where("deleted_at IS NULL AND staged_artifact_id IS NOT NULL").
		Find(&staged).Error; err != nil {
		return nil, err
	}
	if err = m.db.WithContext(ctx).Table(tableNamePagePublications).
		Select("artifact_id::text AS id").
		Where("artifact_id IS NOT NULL").
		Find(&pubs).Error; err != nil {
		return nil, err
	}
	if err = m.db.WithContext(ctx).Table(tableNamePageStagings).
		Select("artifact_id::text AS id").
		Find(&stagings).Error; err != nil {
		return nil, err
	}
	return dedupeStrings(artifactIDsOf(active, staged, pubs, stagings)), nil
}

type artifactIDRow struct {
	ID *string `gorm:"column:id"`
}

func artifactIDsOf(lists ...[]artifactIDRow) []string {
	out := make([]string, 0)
	for _, list := range lists {
		for _, r := range list {
			if r.ID != nil && *r.ID != "" {
				out = append(out, *r.ID)
			}
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
