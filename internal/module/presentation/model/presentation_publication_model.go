package presentationmodel

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const tableNamePresentationPublications = "presentation_publications"

type PublicationEntity struct {
	PresentationID string  `gorm:"column:presentation_id;primaryKey"`
	Lang           string  `gorm:"column:lang;primaryKey"`
	ActivePath     string  `gorm:"column:active_path;not null"`
	ArtifactID     *string `gorm:"column:artifact_id"`
	ArtifactHash   string  `gorm:"column:artifact_hash;not null"`

	ReceiptID   *string   `gorm:"column:receipt_id"`
	PublishedAt time.Time `gorm:"column:published_at;not null"`
	UpdatedAt   time.Time `gorm:"column:update_time;not null"`
}

func (PublicationEntity) TableName() string { return tableNamePresentationPublications }

type PublicationRecord struct {
	PresentationID string
	Lang           string
	ActivePath     string
	ArtifactID     string
	ArtifactHash   string

	ReceiptID   string
	PublishedAt time.Time
}

func (m *Model) PublicationDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationEntity{})
}

func (m *Model) ListPublications(ctx context.Context, presentationID string) (list []PublicationEntity, err error) {
	err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).Order("lang ASC").Find(&list).Error
	return list, err
}

func (m *Model) GetPublication(ctx context.Context, presentationID, lang string) (e *PublicationEntity, err error) {
	e = &PublicationEntity{}
	if err = m.PublicationDB(ctx).Where("presentation_id = ? AND lang = ?", presentationID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) PublishedArtifactIDTx(tx *gorm.DB, presentationID, lang string) (string, error) {
	var rows []struct{ ArtifactID *string }
	err := tx.Model(&PublicationEntity{}).
		Select("artifact_id").
		Where("presentation_id = ? AND lang = ?", presentationID, lang).
		Limit(1).Find(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ArtifactID == nil {
		return "", nil
	}
	return *rows[0].ArtifactID, nil
}

func (m *Model) PublishedReceiptIDTx(tx *gorm.DB, presentationID, lang string) (string, error) {
	var rows []struct{ ReceiptID *string }
	err := tx.Model(&PublicationEntity{}).
		Select("receipt_id").
		Where("presentation_id = ? AND lang = ?", presentationID, lang).
		Limit(1).Find(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ReceiptID == nil {
		return "", nil
	}
	return *rows[0].ReceiptID, nil
}

func (m *Model) PublishedReceiptID(ctx context.Context, presentationID, lang string) (string, error) {
	if strings.TrimSpace(presentationID) == "" || strings.TrimSpace(lang) == "" {
		return "", nil
	}
	var rows []struct{ ReceiptID *string }
	err := m.db.WithContext(ctx).Model(&PublicationEntity{}).
		Select("receipt_id").
		Where("presentation_id = ? AND lang = ?", presentationID, lang).
		Limit(1).Find(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ReceiptID == nil {
		return "", nil
	}
	return *rows[0].ReceiptID, nil
}

func (m *Model) MarkPublishedLang(ctx context.Context, rec PublicationRecord) error {
	if rec.PresentationID == "" || rec.Lang == "" || rec.ActivePath == "" {
		return errors.New("激活记录缺少 presentation_id/lang/active_path")
	}
	row := &PublicationEntity{
		PresentationID: rec.PresentationID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if rec.ReceiptID != "" {
		id := rec.ReceiptID
		row.ReceiptID = &id
	}
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "presentation_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "receipt_id", "published_at", "update_time",
		}),
	}).Create(row).Error
}

func (m *Model) MarkPublishedLangTx(tx *gorm.DB, rec PublicationRecord) error {
	if rec.PresentationID == "" || rec.Lang == "" || rec.ActivePath == "" {
		return errors.New("激活记录缺少 presentation_id/lang/active_path")
	}
	row := &PublicationEntity{
		PresentationID: rec.PresentationID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if rec.ReceiptID != "" {
		id := rec.ReceiptID
		row.ReceiptID = &id
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "presentation_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "receipt_id", "published_at", "update_time",
		}),
	}).Create(row).Error
}

func (m *Model) FindInstanceByActivePath(ctx context.Context, projectID, path, excludeInstanceID string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&InstanceEntity{}).
			Joins("JOIN "+tableNamePresentationPublications+" AS pp ON pp.presentation_id = presentation_instances.id").
			Where("presentation_instances.project_id = ? AND pp.active_path = ?", projectID, path)
		if excludeInstanceID != "" {
			q = q.Where("presentation_instances.id <> ?", excludeInstanceID)
		}
		return q.First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) ListCurrentArtifactIDs(ctx context.Context) (ids []string, err error) {
	ids = []string{}
	var rows []struct {
		ArtifactID *string `gorm:"column:artifact_id"`
	}
	err = m.db.WithContext(ctx).Model(&PublicationEntity{}).
		Select("artifact_id").
		Where("artifact_id IS NOT NULL").
		Distinct().
		Scan(&rows).Error
	if err != nil {
		return ids, err
	}
	for _, r := range rows {
		if r.ArtifactID != nil {
			ids = append(ids, *r.ArtifactID)
		}
	}
	return ids, err
}

func (m *Model) ListPublicationsForInstance(ctx context.Context, presentationID string) (rows []PublicationEntity, err error) {
	if err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).
		Order("lang ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *Model) ListPublicationsTx(tx *gorm.DB, presentationID string) (rows []PublicationEntity, err error) {
	if tx == nil {
		return nil, errors.New("presentation: 账本读取必须在调用方事务内")
	}
	if err = tx.Model(&PublicationEntity{}).Where("presentation_id = ?", presentationID).
		Order("lang ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *Model) ListActivePathsForInstance(ctx context.Context, presentationID string) (paths []string, err error) {
	var rows []PublicationEntity
	if err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ActivePath != "" {
			paths = append(paths, r.ActivePath)
		}
	}
	return paths, nil
}
