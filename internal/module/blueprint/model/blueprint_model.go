package blueprintmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	pageenums "go_wp/internal/module/page/enums"

	"gorm.io/gorm"
)

const (
	tableNameBlueprints        = "blueprints"
	tableNameBlueprintVersions = "blueprint_versions"
)

func IsValidKind(kind string) bool {
	for _, k := range pageenums.PageKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

func PageKinds() []string { return pageenums.PageKinds() }

type BlueprintEntity struct {
	ID            string          `gorm:"column:id;primaryKey"`
	Name          string          `gorm:"column:name;not null"`
	Kind          string          `gorm:"column:kind;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion  int64           `gorm:"column:draft_version;not null"`
	CreatedAt     time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt     time.Time       `gorm:"column:update_time;not null"`
}

func (BlueprintEntity) TableName() string { return tableNameBlueprints }

type VersionEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	BlueprintID string          `gorm:"column:blueprint_id;not null"`
	Version     int64           `gorm:"column:version;not null"`
	Document    json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
}

func (VersionEntity) TableName() string { return tableNameBlueprintVersions }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&BlueprintEntity{})
}

func (m *Model) DBVersion(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VersionEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) Create(ctx context.Context, e *BlueprintEntity) error {
	return m.DB(ctx).Create(e).Error
}

func (m *Model) CreateWithVersion(ctx context.Context, e *BlueprintEntity, v *VersionEntity) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return tx.Create(v).Error
	})
}

func (m *Model) Get(ctx context.Context, id string) (e *BlueprintEntity, err error) {
	var row BlueprintEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) List(ctx context.Context, kind string) (list []*BlueprintEntity, err error) {
	q := m.db.WithContext(ctx).Order("update_time DESC, id DESC")
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	err = q.Find(&list).Error
	return list, err
}

func (m *Model) Save(ctx context.Context, e *BlueprintEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"draft_document": e.DraftDocument,
		"draft_version":  e.DraftVersion,
		"update_time":    e.UpdatedAt,
	}).Error
}

func (m *Model) CreateVersion(ctx context.Context, v *VersionEntity) error {
	return m.DBVersion(ctx).Create(v).Error
}

func (m *Model) SaveWithVersion(ctx context.Context, e *BlueprintEntity, v *VersionEntity) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&BlueprintEntity{}).Where("id = ?", e.ID).Updates(map[string]any{
			"draft_document": e.DraftDocument,
			"draft_version":  e.DraftVersion,
			"update_time":    e.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		return tx.Create(v).Error
	})
}

func (m *Model) LatestVersion(ctx context.Context, blueprintID string) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("blueprint_id = ?", blueprintID).
		Order("version DESC").
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) GetVersion(ctx context.Context, blueprintID string, version int64) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("blueprint_id = ? AND version = ?", blueprintID, version).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) Delete(ctx context.Context, id string) error {
	return m.db.WithContext(ctx).Where("id = ?", id).Delete(&BlueprintEntity{}).Error
}
