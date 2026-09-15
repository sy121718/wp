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
		// 共享内容对象必须**先落**：page_artifact_objects.content_hash 有外键指向
		// content_objects(content_hash)，顺序反过来就是「先插引用、后插被引用行」，
		// 直接外键违例（多语言第二次发布的归档路径踩过）。
		// 事务内 ON CONFLICT DO NOTHING 幂等写入（first-writer-wins）。
		// 冲突 target 收窄到 content_hash：只有「同一内容已登记」才跳过，
		// 其它唯一冲突（例如 object_key 派生错了）必须显式报错而不是静默丢弃。
		if len(contentObjects) > 0 {
			if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "content_hash"}}, DoNothing: true}).CreateInBatches(contentObjects, 100).Error; err != nil {
				return err
			}
		}
		// 闭包：内容对象就位之后再插引用。
		if len(objects) > 0 {
			for i := range objects {
				objects[i].ArtifactID = id
			}
			if err = tx.CreateInBatches(objects, 100).Error; err != nil {
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

// 产物负载状态（与 page_artifacts 的 CHECK 约束逐字对应）。
const (
	// PayloadStateAvailable 产物负载可用。
	PayloadStateAvailable = "available"
	// PayloadStateGCPending 已判定可回收、但物理文件因同 hash 仍被其他行引用而未删除。
	PayloadStateGCPending = "gc_pending"
	// PayloadStateDeleted 物理文件已删除，仅保留元数据（source_document 仍在，
	// 需要时可用 POST /api/page/artifact/rebuild 重建）。
	PayloadStateDeleted = "deleted"
)

// ListPageIDsByOtherRegistryVersion 返回「存在 registry_version 与 current 不同的
// 可用产物」的页面 ID（去重、字典序，确定性输出）。
//
// 用途：部署新组件后的全站待重建识别 —— 组件是编译进二进制的，没有运行时事件
// 能提示「已有产物由旧组件产出」，只能靠产物元数据里的版本号比对。
// 只统计 payload_state='available' 的行：已标记回收的产物不构成重建理由。
func (m *Model) ListPageIDsByOtherRegistryVersion(ctx context.Context, current string) (ids []string, err error) {
	err = m.DB(ctx).
		Where("payload_state = ? AND registry_version <> ?", PayloadStateAvailable, current).
		Distinct().Order("page_id").Pluck("page_id", &ids).Error
	return ids, err
}

// ListByPage 按版本倒序读取页面的全部产物记录（跨语言，含每个语言的各版本行）。
// 有意不按语言过滤：这是「本页产物全景」视图，语言维度由每行 Lang 自带；
// 需要单语言切片时按 GetByPageVersion 或调用方自行过滤。
func (m *Model) ListByPage(ctx context.Context, pageID string) (list []PageArtifactEntity, err error) {
	err = m.DB(ctx).Where("page_id = ?", pageID).Order("version DESC, lang ASC").Find(&list).Error
	return list, err
}

// ListGCCandidates 列出可回收候选：
//   - payload_state = available（已标记回收的不重复处理）
//   - created_at 早于 before（保留窗口之外）
//   - 不在 excludeIDs 内（调用方传入的保护集合：页面指针 / 每语言激活暂存 / 路由指向）
//
// excludeIDs 为空表示调用方无法确定保护集合 —— 此时返回空列表（宁可不回收也不误删）。
func (m *Model) ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []PageArtifactEntity, err error) {
	if len(excludeIDs) == 0 {
		return nil, nil
	}
	q := m.DB(ctx).Where("payload_state = ? AND created_at < ?", PayloadStateAvailable, before)
	q = q.Where("id NOT IN ?", excludeIDs)
	err = q.Order("created_at ASC").Find(&list).Error
	return list, err
}

// CountOtherAvailableByHash 统计同 hash 的**其他** available 行数。
//
// 产物是内容寻址的（artifacts/<hash>/），多条元数据行可能指向同一份文件。
// 只有在没有任何其他可用行引用该 hash 时，删除物理文件才是安全的。
func (m *Model) CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error) {
	err = m.DB(ctx).
		Where("artifact_hash = ? AND payload_state = ? AND id <> ?", hash, PayloadStateAvailable, excludeID).
		Count(&n).Error
	return n, err
}

// orphanContentObjectFilter 判定「无任何现存产物行引用」的 SQL 片段。
//
// 引用真源是 page_artifact_objects（产物 → 内容对象闭包投影，本模块表）：
// 只有 payload_state='deleted' 之外的产物行才算有效引用 —— 已回收的产物行虽然
// 元数据还在（source_document 保留、可 rebuild），但它指向的物理目录已被删除，
// 其闭包对象里的 object_key 同样指向不存在的文件，再算作引用只会让内容对象永不回收。
// rebuild 会走重新归档，届时按需重新写入 content_objects。
//
// 表名在片段里以 content_objects 全名书写：DELETE 与 SELECT 共用这一份判定，
// 保证「看候选」与「真删除」用的是同一条规则（否则会出现预览数 10、实删 3）。
const orphanContentObjectFilter = `NOT EXISTS (
		SELECT 1 FROM page_artifact_objects o
		JOIN page_artifacts a ON a.id = o.artifact_id
		WHERE o.content_hash = content_objects.content_hash AND a.payload_state <> ?
	)`

// orphanContentObjectScope 构造孤儿内容对象的查询范围：
//   - created_at 早于 before（保留窗口之外，避免清理刚落盘、闭包尚未提交的对象）
//   - 不被任何现存产物行引用（见 orphanContentObjectFilter）
//   - hashes 非空时收窄到指定集合（删除时用，避免删掉查完之后才出现的候选）
func (m *Model) orphanContentObjectScope(ctx context.Context, before time.Time, hashes []string) *gorm.DB {
	q := m.db.WithContext(ctx).Table(tableNameContentObjects).
		Where("content_objects.created_at < ?", before).
		Where(orphanContentObjectFilter, PayloadStateDeleted)
	if len(hashes) > 0 {
		q = q.Where("content_objects.content_hash IN ?", hashes)
	}
	return q
}

// CountOrphanContentObjects 统计孤儿内容对象数量（GC 的 dryRun 预演用）。
func (m *Model) CountOrphanContentObjects(ctx context.Context, before time.Time) (n int64, err error) {
	err = m.orphanContentObjectScope(ctx, before, nil).Count(&n).Error
	return n, err
}

// ListOrphanContentObjects 列出孤儿内容对象（按 created_at 升序，至多 limit 条）。
// limit <= 0 时不加限制 —— 调用方负责给一个有限批次。
func (m *Model) ListOrphanContentObjects(ctx context.Context, before time.Time, limit int) (list []ContentObjectEntity, err error) {
	q := m.orphanContentObjectScope(ctx, before, nil).Select("content_objects.*").Order("content_objects.created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&list).Error
	return list, err
}

// deleteOrphanContentObjectsSQL 硬删除孤儿内容对象。
//
// 刻意写原生 DELETE 而不是走 GORM 的 Delete：ContentObjectEntity 有 DeletedAt 字段，
// GORM 会把它当软删除列，Delete 会退化成 UPDATE deleted_at —— 那样"GC 之后仍能查到
// 这些行"，标记清除也就白做了。这里要的是真删。
const deleteOrphanContentObjectsSQL = `DELETE FROM content_objects
	WHERE content_hash IN ?
	  AND created_at < ?
	  AND ` + orphanContentObjectFilter + `
	RETURNING content_hash`

// DeleteOrphanContentObjects 删除给定 hash 中**此刻仍是孤儿**的内容对象，
// 返回真正删掉的 hash 列表（RETURNING）。
//
// 复查与删除在同一条语句里完成（而不是先查后删）：查与删之间若有并发归档复用同一
// 内容对象，两者之间的窗口会让「先查后删」删掉刚被引用的行 —— 语句内 NOT EXISTS
// 交给数据库做原子判定。返回集合而不是行数，是为了让调用方能逐条给出「删了 / 被认领了」
// 的准确结论（只报行数时，差值既可能是并发认领也可能是别的意外）。
func (m *Model) DeleteOrphanContentObjects(ctx context.Context, hashes []string, before time.Time) (deleted []string, err error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	rows, err := m.db.WithContext(ctx).Raw(deleteOrphanContentObjectsSQL, hashes, before, PayloadStateDeleted).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h string
		if serr := rows.Scan(&h); serr != nil {
			return deleted, serr
		}
		deleted = append(deleted, h)
	}
	return deleted, rows.Err()
}

// MarkPayloadState 批量更新负载状态（gc_pending / deleted），返回受影响行数。
func (m *Model) MarkPayloadState(ctx context.Context, ids []string, state string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	updates := map[string]any{"payload_state": state}
	if state == PayloadStateDeleted {
		updates["payload_deleted_at"] = at
	}
	res := m.DB(ctx).Where("id IN ?", ids).Updates(updates)
	return res.RowsAffected, res.Error
}
