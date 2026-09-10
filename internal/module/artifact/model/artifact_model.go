// Package artifactmodel 实现 artifact 模块 page_artifacts、content_objects
// 与 page_artifact_objects 表持久化：产物元数据投影与内容对象闭包。
package artifactmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	tableNamePageArtifacts       = "page_artifacts"
	tableNameContentObjects      = "content_objects"
	tableNamePageArtifactObjects = "page_artifact_objects"
)

// PageArtifactEntity 对应 page_artifacts 表：构建产物的数据库元数据投影。
// UNIQUE(page_id, version, lang) 对齐生产 DDL（init_builder_schema.sql:238 + 迁移
// 061-page-artifacts-lang）：同一草稿版本下「每个语言」各恰一行，同页多语言产物
// 并存互不覆盖；同版本同语言重构建仍为替换语义的 DB 层兜底。
// 索引名 uk_page_artifacts_page_version_lang 与迁移 061 保持一致，AutoMigrate 同名同形。
//
// Lang 是唯一键第三维：空值会让唯一键退化为 (page_id, version) 互相覆盖，
// 因此 service 落库前必须归一化为站点默认语言。
type PageArtifactEntity struct {
	ID                        string          `gorm:"column:id;type:uuid;primaryKey"`
	PageID                    string          `gorm:"column:page_id;type:uuid;not null;uniqueIndex:uk_page_artifacts_page_version_lang"`
	Version                   int64           `gorm:"column:version;not null;uniqueIndex:uk_page_artifacts_page_version_lang"`
	Lang                      string          `gorm:"column:lang;type:text;not null;default:'zh-CN';uniqueIndex:uk_page_artifacts_page_version_lang"`
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
//
// 不带 lang 维度：产物 hash 覆盖 Manifest（含 lang），同 hash 必同语言
// （docs/06-D-site-i18n.md §15.4），因此 (page_id, hash) 已足以定位唯一一行。
// 回滚等只持有 hash 的调用方因此无需知道语言。
func (m *Model) GetByHash(ctx context.Context, pageID, hash string) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("page_id = ? AND artifact_hash = ?", pageID, hash).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// GetByPageVersion 按 (pageID, version, lang) 查询产物记录。
// page_artifacts 以 (page_id, version, lang) 唯一：同页多语言各占一行、互不覆盖；
// 同一语言同一草稿版本重构建（编译器升级导致 hash 变化）时替换该行产物指针。
// lang 由调用方传入（不写死业务条件）；空 lang 匹配不到任何行，service 必须先归一化。
func (m *Model) GetByPageVersion(ctx context.Context, pageID string, version int64, lang string) (e *PageArtifactEntity, err error) {
	e = &PageArtifactEntity{}
	if err = m.DB(ctx).Where("page_id = ? AND version = ? AND lang = ?", pageID, version, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ReplaceArtifactContent 同版本重构建时替换产物指针与归档内容（含对象闭包重建）。
// contentObjects 为需幂等写入的共享内容对象（content_objects），与产物行、闭包
// 在同一事务内提交——此前内容对象在事务外写入，替换失败会残留孤儿行。
func (m *Model) ReplaceArtifactContent(ctx context.Context, id string, entity *PageArtifactEntity, objects []PageArtifactObjectEntity, contentObjects []ContentObjectEntity) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err = tx.Model(&PageArtifactEntity{}).Where("id = ?", id).Updates(map[string]any{
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
		// 共享内容对象：事务内 ON CONFLICT DO NOTHING 幂等写入（first-writer-wins）。
		for i := range contentObjects {
			if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&contentObjects[i]).Error; err != nil {
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

// ListByPage 按版本倒序读取页面的全部产物记录（跨语言，含每个语言的各版本行）。
// 有意不按语言过滤：这是「本页产物全景」视图，语言维度由每行 Lang 自带；
// 需要单语言切片时按 GetByPageVersion 或调用方自行过滤。
func (m *Model) ListByPage(ctx context.Context, pageID string) (list []PageArtifactEntity, err error) {
	err = m.DB(ctx).Where("page_id = ?", pageID).Order("version DESC, lang ASC").Find(&list).Error
	return list, err
}
