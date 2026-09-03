// Package artifactmodel 实现 artifact 模块 page_artifacts、content_objects
// 与 page_artifact_objects 表持久化：产物元数据投影与内容对象闭包。
package artifactmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const (
	tableNamePageArtifacts       = "page_artifacts"
	tableNameContentObjects      = "content_objects"
	tableNamePageArtifactObjects = "page_artifact_objects"
)

// PageArtifactEntity 对应 page_artifacts 表：构建产物的数据库元数据投影。
type PageArtifactEntity struct {
	ID                        string          `gorm:"column:id;type:uuid;primaryKey"`
	PageID                    string          `gorm:"column:page_id;type:uuid;not null"`
	Version                   int64           `gorm:"column:version;not null"`
	SourceDocument            json.RawMessage `gorm:"column:source_document;type:jsonb;not null"`
	PageDocumentSchemaVersion int             `gorm:"column:page_document_schema_version;not null"`
	SourceHash                string          `gorm:"column:source_hash;type:text;not null"`
	BuildInputManifest        json.RawMessage `gorm:"column:build_input_manifest;type:jsonb;not null"`
	BuildInputHash            string          `gorm:"column:build_input_hash;type:text;not null"`
	ArtifactProvider          string          `gorm:"column:artifact_provider;type:text;not null"`
	ArtifactKey               string          `gorm:"column:artifact_key;type:text;not null"`
	ArtifactHash              string          `gorm:"column:artifact_hash;type:text;not null"`
	CompilerVersion           string          `gorm:"column:compiler_version;type:text;not null"`
	RegistryVersion           string          `gorm:"column:registry_version;type:text;not null"`
	Manifest                  json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState              string          `gorm:"column:payload_state;type:text;not null"`
	PayloadDeletedAt          *time.Time      `gorm:"column:payload_deleted_at"`
	Note                      string          `gorm:"column:note;type:text;not null"`
	CreatedBy                 string          `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt                 time.Time       `gorm:"column:created_at;not null"`
}

func (PageArtifactEntity) TableName() string { return tableNamePageArtifacts }

// ContentObjectEntity 对应 content_objects 表：共享内容对象（Locator 投影）。
type ContentObjectEntity struct {
	ContentHash string     `gorm:"column:content_hash;type:text;primaryKey"`
	Provider    string     `gorm:"column:provider;type:text;not null"`
	ObjectKey   string     `gorm:"column:object_key;type:text;not null"`
	ByteSize    int64      `gorm:"column:byte_size;not null"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
}

func (ContentObjectEntity) TableName() string { return tableNameContentObjects }

// PageArtifactObjectEntity 对应 page_artifact_objects 表：产物 → 内容对象闭包。
type PageArtifactObjectEntity struct {
	ArtifactID  string `gorm:"column:artifact_id;type:uuid;primaryKey"`
	ContentHash string `gorm:"column:content_hash;type:text;primaryKey"`
}

func (PageArtifactObjectEntity) TableName() string { return tableNamePageArtifactObjects }

// Model 封装 artifact 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewArtifactModel 创建 Artifact Model。
func NewArtifactModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 page_artifacts 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageArtifactEntity{})
}

// Transaction 在数据库事务中执行给定函数；产物元数据与闭包必须原子提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// GetByHash 按 (pageID, hash) 查询产物记录。
func (m *Model) GetByHash(ctx context.Context, pageID, hash string) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("page_id = ? AND artifact_hash = ?", pageID, hash).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// GetByPageVersion 按 (pageID, version) 查询产物记录。
// page_artifacts 以 (page_id, version) 唯一：同一草稿版本重构建（编译器升级
// 导致 hash 变化）时应替换该行产物指针，而非插入新行。
func (m *Model) GetByPageVersion(ctx context.Context, pageID string, version int64) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("page_id = ? AND version = ?", pageID, version).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ReplaceArtifactContent 同版本重构建时替换产物指针与归档内容（含对象闭包重建）。
func (m *Model) ReplaceArtifactContent(ctx context.Context, id string, entity *PageArtifactEntity, objects []PageArtifactObjectEntity) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err = tx.Model(&PageArtifactEntity{}).Where("id = ?", id).Updates(map[string]interface{}{
			"source_document":              entity.SourceDocument,
			"page_document_schema_version": entity.PageDocumentSchemaVersion,
			"source_hash":                  entity.SourceHash,
			"build_input_manifest":         entity.BuildInputManifest,
			"build_input_hash":             entity.BuildInputHash,
			"artifact_provider":            entity.ArtifactProvider,
			"artifact_key":                 entity.ArtifactKey,
			"artifact_hash":                entity.ArtifactHash,
			"compiler_version":             entity.CompilerVersion,
			"registry_version":             entity.RegistryVersion,
			"manifest":                     entity.Manifest,
		}).Error; err != nil {
			return err
		}
		if err = tx.Where("artifact_id = ?", id).Delete(&PageArtifactObjectEntity{}).Error; err != nil {
			return err
		}
		for i := range objects {
			objects[i].ArtifactID = id
			if err = tx.Create(&objects[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetByID 按产物行 ID 查询产物记录。
func (m *Model) GetByID(ctx context.Context, id string) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListByPage 按版本倒序读取页面的全部产物记录。
func (m *Model) ListByPage(ctx context.Context, pageID string) (list []PageArtifactEntity, err error) {
	err = m.DB(ctx).Where("page_id = ?", pageID).Order("version DESC").Find(&list).Error
	return list, err
}
