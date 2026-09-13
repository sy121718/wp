// Package presentationmodel 实现 presentation 模块四张表持久化（0-A2）。
//
// DDL 对齐（本轮修复）：实体列集合严格对齐生产 DDL
// public/migrations/init_builder_schema.sql —— presentation_instances /
// document_snapshots / presentation_artifacts / presentation_dependencies。
//
// 修复前 model 按 status / artifact_hash / source_entity_revision 读写，
// 生产库没有这三列（真实列是 stale / staged_artifact_id / active_artifact_id
// 与 source_entity_revision_id），CreateInstance/UpdateInstance 对真实库必然
// 失败；测试靠 AutoMigrate 补列掩盖了缺陷（见 public/test/presentation/unit
// 的列集合断言）。
package presentationmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const (
	tableNamePresentationInstances    = "presentation_instances"
	tableNameDocumentSnapshots        = "document_snapshots"
	tableNamePresentationArtifacts    = "presentation_artifacts"
	tableNamePresentationDependencies = "presentation_dependencies"
)

// InstanceEntity presentation_instances 表实体。
//
// 语义（对齐 DDL，非旧 model 的 status/artifact_hash）：
//   - 发布状态由指针列承载：staged_artifact_id 暂存、active_artifact_id 已上线；
//   - stale 表示「依赖已变更、待重建」；
//   - project_id / template_id 为 NOT NULL 外键，装配时必须落库。
type InstanceEntity struct {
	ID                string  `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID         string  `gorm:"column:project_id;type:uuid;not null"`
	EntityType        string  `gorm:"column:entity_type;not null"`
	EntityID          string  `gorm:"column:entity_id;type:uuid;not null"`
	URLPath           string  `gorm:"column:url_path;not null"`
	TemplateID        string  `gorm:"column:template_id;type:uuid;not null"`
	CurrentSnapshotID *string `gorm:"column:current_snapshot_id;type:uuid"`
	StagedSnapshotID  *string `gorm:"column:staged_snapshot_id;type:uuid"`
	StagedArtifactID  *string `gorm:"column:staged_artifact_id;type:uuid"`
	ActiveArtifactID  *string `gorm:"column:active_artifact_id;type:uuid"`
	Stale             bool    `gorm:"column:stale;not null"`
	// DeletedAt 保留列（本轮不启用软删语义，删除走聚合内级联硬删）。
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
}

// TableName 表名。
func (InstanceEntity) TableName() string { return tableNamePresentationInstances }

// SnapshotEntity document_snapshots 表实体。
//
// SourceEntityRevisionID 对应真实列 source_entity_revision_id（uuid NOT NULL）：
// 指向产生本快照的内容实体 ID（当前无独立的 revision 行表，落实体 ID）。
type SnapshotEntity struct {
	ID                      string          `gorm:"column:id;type:uuid;primaryKey"`
	PresentationInstanceID  string          `gorm:"column:presentation_instance_id;type:uuid;not null"`
	SourceTemplateVersionID string          `gorm:"column:source_template_version_id;type:uuid;not null"`
	SourceEntityRevisionID  string          `gorm:"column:source_entity_revision_id;type:uuid;not null"`
	Document                json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt               time.Time       `gorm:"column:created_at;not null"`
}

// TableName 表名。
func (SnapshotEntity) TableName() string { return tableNameDocumentSnapshots }

// ArtifactEntity presentation_artifacts 表实体（自动发布实例的产物元数据）。
type ArtifactEntity struct {
	ID                     string          `gorm:"column:id;type:uuid;primaryKey"`
	PresentationInstanceID string          `gorm:"column:presentation_instance_id;type:uuid;not null"`
	SnapshotID             string          `gorm:"column:snapshot_id;type:uuid;not null"`
	Version                int64           `gorm:"column:version;not null"`
	SourceHash             string          `gorm:"column:source_hash;not null"`
	BuildInputManifest     json.RawMessage `gorm:"column:build_input_manifest;type:jsonb;not null"`
	BuildInputHash         string          `gorm:"column:build_input_hash;not null"`
	ArtifactProvider       string          `gorm:"column:artifact_provider;not null"`
	ArtifactKey            string          `gorm:"column:artifact_key;not null"`
	ArtifactHash           string          `gorm:"column:artifact_hash;not null"`
	CompilerVersion        string          `gorm:"column:compiler_version;not null"`
	RegistryVersion        string          `gorm:"column:registry_version;not null"`
	Manifest               json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState           string          `gorm:"column:payload_state;not null"`
	PayloadDeletedAt       *time.Time      `gorm:"column:payload_deleted_at"`
	Note                   string          `gorm:"column:note;not null"`
	CreatedBy              string          `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt              time.Time       `gorm:"column:created_at;not null"`
}

// TableName 表名。
func (ArtifactEntity) TableName() string { return tableNamePresentationArtifacts }

// DependencyEntity presentation_dependencies 行（产物声明的构建期依赖）。
type DependencyEntity struct {
	PresentationID string    `gorm:"column:presentation_id;type:uuid;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id;type:uuid;primaryKey"`
	DependencyKind string    `gorm:"column:dependency_kind;primaryKey"`
	DependencyKey  string    `gorm:"column:dependency_key;primaryKey"`
	Revision       *string   `gorm:"column:revision"`
	LastChecked    time.Time `gorm:"column:last_checked;not null"`
}

// TableName 表名。
func (DependencyEntity) TableName() string { return tableNamePresentationDependencies }

// Model 四张表数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// InstanceDB 绑定实例表。
func (m *Model) InstanceDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&InstanceEntity{})
}

// SnapshotDB 绑定快照表。
func (m *Model) SnapshotDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SnapshotEntity{})
}

// ArtifactDB 绑定产物表。
func (m *Model) ArtifactDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ArtifactEntity{})
}

// DependencyDB 绑定依赖表。
func (m *Model) DependencyDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&DependencyEntity{})
}

// Transaction 透传事务（service 编排聚合内原子写入）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// CreateInstance 新增实例。
func (m *Model) CreateInstance(ctx context.Context, e *InstanceEntity) error {
	return m.InstanceDB(ctx).Create(e).Error
}

// GetInstance 按 ID 查询。
func (m *Model) GetInstance(ctx context.Context, id string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetInstanceByEntity 按内容实体查询。
func (m *Model) GetInstanceByEntity(ctx context.Context, entityType, entityID string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	if err = m.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListInstances 按类型列表。
func (m *Model) ListInstances(ctx context.Context, entityType string) (list []*InstanceEntity, err error) {
	q := m.db.WithContext(ctx).Order("updated_at DESC, id DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	err = q.Find(&list).Error
	return list, err
}

// UpdateInstancePointers 更新实例指针（快照/产物/stale/发布时间）。
//
// 显式列白名单而非 Save：实例行的 project_id/template_id/entity_* 是不可变
// 身份列，重建只允许改指针与状态，防止整体覆盖时误改身份。
func (m *Model) UpdateInstancePointers(ctx context.Context, e *InstanceEntity) error {
	return m.InstanceDB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"current_snapshot_id": e.CurrentSnapshotID,
		"staged_snapshot_id":  e.StagedSnapshotID,
		"staged_artifact_id":  e.StagedArtifactID,
		"active_artifact_id":  e.ActiveArtifactID,
		"stale":               e.Stale,
		"published_at":        e.PublishedAt,
		"updated_at":          e.UpdatedAt,
	}).Error
}

// MarkStale 批量标记实例待重建（依赖失效后的落库动作）。
func (m *Model) MarkStale(ctx context.Context, ids []string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := m.InstanceDB(ctx).Where("id IN ?", ids).Updates(map[string]any{
		"stale": true, "updated_at": at,
	})
	return res.RowsAffected, res.Error
}

// DeleteInstance 删除实例及其聚合内从属行（解引用 → 依赖 → 产物 → 快照 → 实例）。
//
// 必须级联：presentation_artifacts / document_snapshots / presentation_dependencies
// 均以复合外键引用实例行且无 ON DELETE CASCADE，直接 DELETE 实例会被外键拒绝。
// 四张表同属本模块，属 model 层允许的「聚合内原子组合」。
//
// 必须先解引用：实例的 staged/active_artifact_id 与 current/staged_snapshot_id
// 反向引用产物与快照行，形成循环外键（presentation_instances_staged_artifact_fk
// 等），不清空指针就删产物会被这些外键挡住。
func (m *Model) DeleteInstance(ctx context.Context, id string) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&InstanceEntity{}).Where("id = ?", id).Updates(map[string]any{
			"current_snapshot_id": nil,
			"staged_snapshot_id":  nil,
			"staged_artifact_id":  nil,
			"active_artifact_id":  nil,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("presentation_id = ?", id).Delete(&DependencyEntity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("presentation_instance_id = ?", id).Delete(&ArtifactEntity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("presentation_instance_id = ?", id).Delete(&SnapshotEntity{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&InstanceEntity{}).Error
	})
}

// CreateSnapshot 写快照。
func (m *Model) CreateSnapshot(ctx context.Context, e *SnapshotEntity) error {
	return m.SnapshotDB(ctx).Create(e).Error
}

// NextArtifactVersion 取该实例下一个产物版本号（version 在实例内唯一）。
func (m *Model) NextArtifactVersion(ctx context.Context, instanceID string) (v int64, err error) {
	var maxVersion *int64
	if err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("presentation_instance_id = ?", instanceID).
		Select("MAX(version)").Scan(&maxVersion).Error; err != nil {
		return 0, err
	}
	if maxVersion == nil {
		return 1, nil
	}
	return *maxVersion + 1, nil
}

// CreateArtifact 写产物行。
func (m *Model) CreateArtifact(ctx context.Context, e *ArtifactEntity) error {
	return m.ArtifactDB(ctx).Create(e).Error
}

// GetArtifactByHash 按 (实例, 产物哈希) 查询（重建幂等：同字节不新增行）。
func (m *Model) GetArtifactByHash(ctx context.Context, instanceID, hash string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = m.db.WithContext(ctx).
		Where("presentation_instance_id = ? AND artifact_hash = ?", instanceID, hash).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetArtifact 按产物 ID 查询。
func (m *Model) GetArtifact(ctx context.Context, id string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ReplaceDependencies 全量替换某产物的依赖记录（同一事务内 delete + insert）。
func (m *Model) ReplaceDependencies(ctx context.Context, artifactID string, rows []DependencyEntity) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if derr := tx.Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
			return derr
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, 200).Error
	})
}

// ---- 事务句柄变体（service 编排「快照 → 产物行 → 实例指针 → 依赖」四步原子写入）----
//
// 四步跨四张表，任一中间失败都会留下自相矛盾的实例状态（例如产物行已写、
// active_artifact_id 仍指向旧产物，或依赖记录指向不存在的产物）。事务边界
// 由 service 决定，model 只提供接受外部 *gorm.DB 的变体（AGENTS.md model 层定位）。

// CreateSnapshotTx 事务内写快照。
func (m *Model) CreateSnapshotTx(tx *gorm.DB, e *SnapshotEntity) error {
	return tx.Create(e).Error
}

// GetArtifactByHashTx 事务内按 (实例, 产物哈希) 查询。
func (m *Model) GetArtifactByHashTx(tx *gorm.DB, instanceID, hash string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = tx.Where("presentation_instance_id = ? AND artifact_hash = ?", instanceID, hash).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// NextArtifactVersionTx 事务内取下一个产物版本号（MAX+1；调用方需持有实例级互斥）。
func (m *Model) NextArtifactVersionTx(tx *gorm.DB, instanceID string) (v int64, err error) {
	var maxVersion *int64
	if err = tx.Model(&ArtifactEntity{}).
		Where("presentation_instance_id = ?", instanceID).
		Select("MAX(version)").Scan(&maxVersion).Error; err != nil {
		return 0, err
	}
	if maxVersion == nil {
		return 1, nil
	}
	return *maxVersion + 1, nil
}

// CreateArtifactTx 事务内写产物行。
func (m *Model) CreateArtifactTx(tx *gorm.DB, e *ArtifactEntity) error {
	return tx.Create(e).Error
}

// UpdateInstancePointersTx 事务内更新实例指针（列白名单同 UpdateInstancePointers）。
func (m *Model) UpdateInstancePointersTx(tx *gorm.DB, e *InstanceEntity) error {
	return tx.Model(&InstanceEntity{}).Where("id = ?", e.ID).Updates(map[string]any{
		"current_snapshot_id": e.CurrentSnapshotID,
		"staged_snapshot_id":  e.StagedSnapshotID,
		"staged_artifact_id":  e.StagedArtifactID,
		"active_artifact_id":  e.ActiveArtifactID,
		"stale":               e.Stale,
		"published_at":        e.PublishedAt,
		"updated_at":          e.UpdatedAt,
	}).Error
}

// UpdateInstanceTemplateTx 事务内改写实例绑定的模板（issue #14：「详情页模板可选」）。
//
// template_id 曾是只读身份列（见 UpdateInstancePointers 注释），本票起它是**可切换的绑定**：
// 切换必须与本次重建的快照/产物/指针在同一事务里落库，否则会出现「产物来自新模板、
// 实例仍记着旧模板」的漂移，下一次重建又会退回旧模板。
func (m *Model) UpdateInstanceTemplateTx(tx *gorm.DB, id, templateID string, at time.Time) error {
	return tx.Model(&InstanceEntity{}).Where("id = ?", id).Updates(map[string]any{
		"template_id": templateID,
		"updated_at":  at,
	}).Error
}

// UpdateInstanceURLTx 事务内改写实例的线上路径（改 URL）。
//
// url_path 曾是只读身份列（见 UpdateInstancePointers 注释：project_id /
// template_id / entity_* 不可变），本次起它是**可变更的站点事实**，与手工页面的
// draft_path 同一性质：内容（实体、模板、快照）不变，路径可以搬。
// 改动必须与本次重建的产物行/指针在同一事务里落库，否则会出现「产物烘的是新
// 路径 canonical、实例仍指向旧路径」的漂移，且下次重建会退回旧路径。
//
// 只改 url_path 一列：归属身份（project_id / entity_type / entity_id /
// template_id）不动 —— 改 URL 不是换实体。
func (m *Model) UpdateInstanceURLTx(tx *gorm.DB, id, urlPath string, at time.Time) error {
	return tx.Model(&InstanceEntity{}).Where("id = ?", id).Updates(map[string]any{
		"url_path":   urlPath,
		"updated_at": at,
	}).Error
}

// FindInstanceByPath 查同工程内占用该路径的其他展示实例（改 URL 的占用预检）。
//
// 为什么必须查本表而不只查 page_routes：详情页实例的 URL 占用登记进 page_routes
// 是本次才补上的，历史实例在路由表里没有行；以本表为真源，预检不依赖路由登记的
// 完整性（漏登只影响「别人防我」，不影响「我防别人」）。返回 ErrRecordNotFound
// 表示路径空闲。
func (m *Model) FindInstanceByPath(ctx context.Context, projectID, urlPath, excludeInstanceID string) (e *InstanceEntity, err error) {
	q := m.InstanceDB(ctx).
		Where("project_id = ? AND url_path = ? AND deleted_at IS NULL", projectID, urlPath)
	if excludeInstanceID != "" {
		q = q.Where("id <> ?", excludeInstanceID)
	}
	var row InstanceEntity
	if err = q.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ReplaceDependenciesTx 事务内全量替换依赖记录。
func (m *Model) ReplaceDependenciesTx(tx *gorm.DB, artifactID string, rows []DependencyEntity) error {
	if err := tx.Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(rows, 200).Error
}

// ListDependencies 读取某产物的全部依赖记录（测试与诊断用，按 kind,key 排序）。
func (m *Model) ListDependencies(ctx context.Context, artifactID string) (list []DependencyEntity, err error) {
	err = m.DependencyDB(ctx).Where("artifact_id = ?", artifactID).
		Order("dependency_kind, dependency_key").Find(&list).Error
	return list, err
}

// MarkStaleByDependency 按依赖源 (kind,key) 精确标记受影响实例待重建，
// 返回受影响的实例 ID（去重、升序）。
//
// 命中条件：该实例的**活跃或暂存**产物在依赖表里声明了这条依赖
// （与 page 侧同一口径，见 page/model/page_dependency_model.go）。
func (m *Model) MarkStaleByDependency(ctx context.Context, kind, key string, at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Raw(`
		WITH affected AS (
			SELECT DISTINCT d.presentation_id AS presentation_id
			FROM presentation_dependencies d
			JOIN presentation_instances p ON p.id = d.presentation_id
			WHERE d.dependency_kind = ?
			  AND d.dependency_key = ?
			  AND p.deleted_at IS NULL
			  AND d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)
		)
		UPDATE presentation_instances SET stale = true, updated_at = ?
		WHERE deleted_at IS NULL AND id IN (SELECT presentation_id FROM affected)
		RETURNING id`, kind, key, at).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}
