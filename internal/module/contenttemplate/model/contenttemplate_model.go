package contenttemplatemodel

import (
	"cmp"
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"slices"
	"strings"
	"time"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const (
	tableNameContentTemplates        = "content_templates"
	tableNameContentTemplateVersions = "content_template_versions"

	tableNameContentTemplatePins = "content_template_component_pins"
)

const (
	TemplateRoleDetail = contenttemplatecontract.TemplateRoleDetail

	TemplateRoleArchive = contenttemplatecontract.TemplateRoleArchive
)

func IsValidTemplateRole(role string) bool {
	return role == TemplateRoleDetail || role == TemplateRoleArchive
}

const (
	EntityTypeHeader = contenttemplatecontract.EntityTypeHeader

	EntityTypeFooter = contenttemplatecontract.EntityTypeFooter
)

func IsStructureTemplateType(entityType string) bool {
	return contenttemplatecontract.IsStructureTemplateType(entityType)
}

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

type TemplateEntity struct {
	ID         string `gorm:"column:id;primaryKey"`
	ProjectID  string `gorm:"column:project_id;not null"`
	Name       string `gorm:"column:name;not null"`
	EntityType string `gorm:"column:entity_type;not null"`

	TemplateRole  string          `gorm:"column:template_role;not null;default:detail"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion  int64           `gorm:"column:draft_version;not null"`

	CurrentVersionID *string `gorm:"column:current_version_id"`

	IsDefault bool      `gorm:"column:is_default;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

func (TemplateEntity) TableName() string { return tableNameContentTemplates }

type VersionEntity struct {
	ID         string          `gorm:"column:id;primaryKey"`
	TemplateID string          `gorm:"column:template_id;not null"`
	Version    int64           `gorm:"column:version;not null"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`

	SourceHash string    `gorm:"column:source_hash;not null"`
	CreatedBy  string    `gorm:"column:created_by;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
}

func (VersionEntity) TableName() string { return tableNameContentTemplateVersions }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

type StructureRefRow struct {
	ID            string          `gorm:"column:id"`
	Name          string          `gorm:"column:name"`
	DraftDocument json.RawMessage `gorm:"column:draft_document"`
}

func (m *Model) ListStructureBindingRows(ctx context.Context, projectID, templateID string) (rows []StructureRefRow, err error) {
	if projectID == "" || templateID == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameContentTemplates).
			Select("id, name, draft_document").
			Where(`project_id = ? AND id <> ?
				AND (draft_document->'settings'->'structure'->>'headerTemplateId' = ?
				 OR draft_document->'settings'->'structure'->>'footerTemplateId' = ?
				 OR (jsonb_typeof(draft_document->'settings'->'structure'->'slotTemplates') = 'object'
				     AND EXISTS (SELECT 1 FROM jsonb_each_text(draft_document->'settings'->'structure'->'slotTemplates') AS kv(slot, tid)
				                 WHERE kv.tid = ?)))`,
				projectID, templateID, templateID, templateID, templateID).
			Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&TemplateEntity{})
}

func (m *Model) DBVersion(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VersionEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) Create(ctx context.Context, e *TemplateEntity) error {

	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&TemplateEntity{}).Create(e).Error
	})
}

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

func (m *Model) CreateVersion(ctx context.Context, v *VersionEntity) error {
	return m.DBVersion(ctx).Create(v).Error
}

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

func (m *Model) SaveWithVersion(ctx context.Context, projectID string, v *VersionEntity, e *TemplateEntity) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.SaveWithVersionTx(ctx, tx, projectID, e.DraftVersion-1, v, e)
	})
}

func (m *Model) DeleteWithHistory(ctx context.Context, projectID, id string) error {

	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		if err := tx.Table(tableNameContentTemplatePins).Where("template_id = ?", id).Delete(nil).Error; err != nil {
			return err
		}
		if err := tx.Where("template_id = ?", id).Delete(&VersionEntity{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&TemplateEntity{}).Error
	})
}

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

func (m *Model) GetVersion(ctx context.Context, templateID string, version int64) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("template_id = ? AND version = ?", templateID, version).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

var ErrProjectRequired = errors.New("contenttemplate: 需要显式工程作用域")

type BlockDocRefRow struct {
	ID          string `gorm:"column:id"`
	Name        string `gorm:"column:name"`
	Version     int64  `gorm:"column:version"`
	FromVersion bool   `gorm:"column:from_version"`
}

func (m *Model) ListBlockDocumentRefs(ctx context.Context, projectID, blockID string) (rows []BlockDocRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		var draft []BlockDocRefRow
		if err = tx.Table("content_templates AS t").
			Select("t.id::text AS id, t.name, 0::bigint AS version, false AS from_version").
			Where("t.project_id = ?", projectID).
			Where("jsonb_path_query_array(t.draft_document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Scan(&draft).Error; err != nil {
			return err
		}
		var hist []BlockDocRefRow
		if err = tx.Table("content_template_versions AS v").
			Select("t.id::text AS id, t.name, v.version AS version, true AS from_version").
			Joins("JOIN content_templates AS t ON t.id = v.template_id").
			Where("t.project_id = ?", projectID).
			Where("jsonb_path_query_array(v.document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Scan(&hist).Error; err != nil {
			return err
		}
		rows = append(draft, hist...)

		slices.SortStableFunc(rows, func(a, b BlockDocRefRow) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version))
		})
		return nil
	})
	return rows, err
}
