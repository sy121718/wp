package presentationmodel

// presentation_publication_model.go — 自动发布实例「每语言激活状态」（I18N-013）。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const tableNamePresentationPublications = "presentation_publications"

// PublicationEntity 对应 presentation_publications 表。
type PublicationEntity struct {
	PresentationID string    `gorm:"column:presentation_id;type:uuid;primaryKey"`
	Lang           string    `gorm:"column:lang;type:text;primaryKey"`
	ActivePath     string    `gorm:"column:active_path;type:text;not null"`
	ArtifactID     *string   `gorm:"column:artifact_id;type:uuid"`
	ArtifactHash   string    `gorm:"column:artifact_hash;type:text;not null"`
	PublishedAt    time.Time `gorm:"column:published_at;not null"`
	UpdatedAt      time.Time `gorm:"column:update_time;not null"`
}

func (PublicationEntity) TableName() string { return tableNamePresentationPublications }

// PublicationRecord 单语言激活状态写入参数。
type PublicationRecord struct {
	PresentationID string
	Lang           string
	ActivePath     string
	ArtifactID     string
	ArtifactHash   string
	PublishedAt    time.Time
}

// PublicationDB 绑定 presentation_publications 表。
func (m *Model) PublicationDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationEntity{})
}

// ListPublications 列出实例全部语言的激活状态。
func (m *Model) ListPublications(ctx context.Context, presentationID string) (list []PublicationEntity, err error) {
	err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).Order("lang ASC").Find(&list).Error
	return list, err
}

// GetPublication 按 (presentation_id, lang) 查询。
func (m *Model) GetPublication(ctx context.Context, presentationID, lang string) (e *PublicationEntity, err error) {
	e = &PublicationEntity{}
	if err = m.PublicationDB(ctx).Where("presentation_id = ? AND lang = ?", presentationID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// MarkPublishedLang 写入某语言激活状态（upsert）。
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
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "presentation_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time",
		}),
	}).Create(row).Error
}

// MarkPublishedLangTx 事务内 upsert。
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
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "presentation_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "published_at", "update_time",
		}),
	}).Create(row).Error
}

// FindInstanceByActivePath 按某语言的已激活访问路径查占用实例（占用预检）。
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

// ListActivePathsForInstance 返回实例全部已登记语言的激活路径。
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
