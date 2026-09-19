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
	"errors"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const (
	tableNamePresentationInstances    = "presentation_instances"
	tableNameDocumentSnapshots        = "document_snapshots"
	tableNamePresentationArtifacts    = "presentation_artifacts"
	tableNamePresentationDependencies = "presentation_dependencies"
)

// 实例角色（审计 EDT-004）：同一个实体可以同时有这两张页面。
const (
	// InstanceRoleDetail 实体详情页（既有语义，默认值）。
	InstanceRoleDetail = "detail"
	// InstanceRoleArchive 归档列表页（如「某分类下的商品列表」）。
	InstanceRoleArchive = "archive"
)

// IsValidInstanceRole 角色是否合法。
func IsValidInstanceRole(role string) bool {
	return role == InstanceRoleDetail || role == InstanceRoleArchive
}

// InstanceEntity presentation_instances 表实体。
//
// 语义（对齐 DDL，非旧 model 的 status/artifact_hash）：
//   - 发布状态由指针列承载：staged_artifact_id 暂存、active_artifact_id 已上线；
//   - stale 表示「依赖已变更、待重建」；
//   - project_id / template_id 为 NOT NULL 外键，装配时必须落库。
type InstanceEntity struct {
	ID         string `gorm:"column:id;primaryKey"`
	ProjectID  string `gorm:"column:project_id;not null"`
	EntityType string `gorm:"column:entity_type;not null"`
	EntityID   string `gorm:"column:entity_id;not null"`
	// InstanceRole 实例角色（审计 EDT-004）：detail = 实体详情页，archive = 归档列表页。
	// 同一个分类可以同时有这两张页面，所以唯一键是（实体 + 角色）而不是实体。
	InstanceRole string `gorm:"column:instance_role;not null;default:detail"`
	URLPath      string `gorm:"column:url_path;not null"`
	TemplateID   string `gorm:"column:template_id;not null"`
	// OverrideDocument 实例级文档覆盖（迁移 281，docs/04-C-instance-override.md）。
	// NULL = 跟随模板（既有行为不变）；非空 = 发布/重建以此文档为准，
	// binding 照常经 ContentResolver 解析，实体数据更新后重建不丢自定义。
	// json.RawMessage gorm 无法推断列型，保留 type 标签属 model 不声明列型的例外清单。
	OverrideDocument json.RawMessage `gorm:"column:override_document;type:jsonb"`
	// RenderMode 渲染模式（迁移 282，商品页双轨）：template=跟随绑定模板（默认，
	// 模板更新可全局下发）| document=该商品独立文档（override_document）。
	// 取值与判定见 RenderModeTemplate / RenderModeDocument 与 NormalizeRenderMode。
	RenderMode        string  `gorm:"column:render_mode;not null;default:template"`
	CurrentSnapshotID *string `gorm:"column:current_snapshot_id"`
	StagedSnapshotID  *string `gorm:"column:staged_snapshot_id"`
	StagedArtifactID  *string `gorm:"column:staged_artifact_id"`
	ActiveArtifactID  *string `gorm:"column:active_artifact_id"`
	Stale             bool    `gorm:"column:stale;not null"`
	// DeletedAt 保留列（本轮不启用软删语义，删除走聚合内级联硬删）。
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	CreatedAt   time.Time  `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time  `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (InstanceEntity) TableName() string { return tableNamePresentationInstances }

// SnapshotEntity document_snapshots 表实体。
//
// SourceEntityRevisionID 对应真实列 source_entity_revision_id（uuid NOT NULL）：
// 指向产生本快照的内容实体 ID（当前无独立的 revision 行表，落实体 ID）。
type SnapshotEntity struct {
	ID                      string          `gorm:"column:id;primaryKey"`
	PresentationInstanceID  string          `gorm:"column:presentation_instance_id;not null"`
	SourceTemplateVersionID string          `gorm:"column:source_template_version_id;not null"`
	SourceEntityRevisionID  string          `gorm:"column:source_entity_revision_id;not null"`
	Document                json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt               time.Time       `gorm:"column:create_time;not null"`
}

// TableName 表名。
func (SnapshotEntity) TableName() string { return tableNameDocumentSnapshots }

// ArtifactEntity presentation_artifacts 表实体（自动发布实例的产物元数据）。
type ArtifactEntity struct {
	ID                     string `gorm:"column:id;primaryKey"`
	PresentationInstanceID string `gorm:"column:presentation_instance_id;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:1"`
	SnapshotID             string `gorm:"column:snapshot_id;not null"`
	Version                int64  `gorm:"column:version;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:2"`
	// Lang 构建语言（I18N-013）：同版本多语言各占一行。
	Lang               string          `gorm:"column:lang;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:3"`
	SourceHash         string          `gorm:"column:source_hash;not null"`
	BuildInputManifest json.RawMessage `gorm:"column:build_input_manifest;type:jsonb;not null"`
	BuildInputHash     string          `gorm:"column:build_input_hash;not null"`
	ArtifactProvider   string          `gorm:"column:artifact_provider;not null"`
	ArtifactKey        string          `gorm:"column:artifact_key;not null"`
	ArtifactHash       string          `gorm:"column:artifact_hash;not null"`
	CompilerVersion    string          `gorm:"column:compiler_version;not null"`
	RegistryVersion    string          `gorm:"column:registry_version;not null"`
	Manifest           json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState       string          `gorm:"column:payload_state;not null"`
	PayloadDeletedAt   *time.Time      `gorm:"column:payload_deleted_at"`
	Note               string          `gorm:"column:note;not null"`
	CreatedBy          string          `gorm:"column:created_by;not null"`
	CreatedAt          time.Time       `gorm:"column:create_time;not null"`
}

// TableName 表名。
func (ArtifactEntity) TableName() string { return tableNamePresentationArtifacts }

// DependencyEntity presentation_dependencies 行（产物声明的构建期依赖）。
type DependencyEntity struct {
	PresentationID string    `gorm:"column:presentation_id;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id;primaryKey"`
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
// RLS（迁移 215）：presentation_instances 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *Model) CreateInstance(ctx context.Context, e *InstanceEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).Create(e).Error
	})
}

// GetInstance 按 ID 查询（工程作用域内）。
//
// projectID 是**必填**（DB-009 第二批）：presentation_instances 带 FORCE 策略，
// 不设 app.project_id 的读取在非超级角色下静默 0 行（fail closed，不报错），
// 表现为「实例突然找不到了」。查询同时显式带 project_id 条件，
// 与策略形成纵深（应用层漏条件时数据库层仍兜底）。
func (m *Model) GetInstance(ctx context.Context, projectID, id string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetInstanceByEntity 按内容实体查询。
func (m *Model) GetInstanceByEntity(ctx context.Context, projectID, entityType, entityID string) (e *InstanceEntity, err error) {
	return m.GetInstanceByEntityRole(ctx, projectID, entityType, entityID, "detail")
}

// GetInstanceByEntityRole 按实体与**角色**查实例（审计 EDT-004）。
//
// 同一个分类既有详情页实例、也可能有归档页实例。只按实体查会把先建的当成
// 「已存在」直接返回 —— 于是「给分类建归档页」这个动作静默变成「拿到详情页实例」，
// 而调用方以为建成了。
func (m *Model) GetInstanceByEntityRole(ctx context.Context, projectID, entityType, entityID, role string) (e *InstanceEntity, err error) {
	if role == "" {
		role = "detail"
	}
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND instance_role = ?",
				projectID, entityType, entityID, role).
			First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListInstances 列出**本工程**的实例（entityType 非空时再按类型过滤）。
//
// 工程维度是必选而不是可选：presentation_instances 带 FORCE 策略，无作用域的
// 全表扫描在非超级角色下返回空集（fail closed），而「列表空」是最难归因的一种表现。
func (m *Model) ListInstances(ctx context.Context, projectID, entityType string) (list []*InstanceEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&InstanceEntity{}).Where("project_id = ?", projectID).
			Order("update_time DESC, id DESC")
		if entityType != "" {
			q = q.Where("entity_type = ?", entityType)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// UpdateInstancePointers 更新实例指针（快照/产物/stale/发布时间）。
//
// 显式列白名单而非 Save：实例行的 project_id/template_id/entity_* 是不可变
// 身份列，重建只允许改指针与状态，防止整体覆盖时误改身份。
func (m *Model) UpdateInstancePointers(ctx context.Context, e *InstanceEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("id = ? AND project_id = ?", e.ID, e.ProjectID).Updates(map[string]any{
			"current_snapshot_id": e.CurrentSnapshotID,
			"staged_snapshot_id":  e.StagedSnapshotID,
			"staged_artifact_id":  e.StagedArtifactID,
			"active_artifact_id":  e.ActiveArtifactID,
			"stale":               e.Stale,
			"published_at":        e.PublishedAt,
			"update_time":         e.UpdatedAt,
		}).Error
	})
}

// MarkStale 批量标记实例待重建（依赖失效后的落库动作）。
func (m *Model) MarkStale(ctx context.Context, projectID string, ids []string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res := tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND id IN ?", projectID, ids).
			Updates(map[string]any{"stale": true, "update_time": at})
		n = res.RowsAffected
		return res.Error
	})
	return n, err
}

// MarkStaleForI18n 把**本工程内**全部实例标记为待重建（文案词条 / 内容译文变更后调用）。
//
// 与 page 侧同名方法同义（page_model.go §MarkStaleForI18n）：组件固定文案与作者文案
// 都在构建期取词注入 HTML 字节，sys_i18n / sys_translation 变化后实例产物都可能过期。
// projectID 必填（DB-009）：presentation_instances 带 FORCE 策略，无作用域的全表
// UPDATE 在换非超级角色后匹配 0 行且不报错 —— 译文改了、商品页却一直是旧字节。
func (m *Model) MarkStaleForI18n(ctx context.Context, projectID string, at time.Time) (n int64, err error) {
	if projectID == "" {
		return 0, errors.New("project id is required")
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var merr error
		n, merr = m.markStaleForI18nIn(ctx, tx, projectID, at)
		return merr
	})
	return n, err
}

// markStaleForI18nIn 整站标记的 SQL 本体：MarkStaleForI18n（自带事务）与
// MarkStaleForI18nTx（在调用方事务内，见下）两条入口共用同一段语句 —— 两处各写一遍
// 会让「自带事务」与「透传事务」两条路径对同一批实例产生不同的标记结果。
func (m *Model) markStaleForI18nIn(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (n int64, err error) {
	res := tx.WithContext(ctx).Model(&InstanceEntity{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Updates(map[string]any{"stale": true, "update_time": at})
	return res.RowsAffected, res.Error
}

// MarkStaleForI18nTx 在**外部事务**内把本工程全部未删除实例标记为待重建
// （page 的译文失效扇出用，见 page 侧 I18nStalePeer 的 Tx 变体）。
//
// 与 MarkStaleForI18n 是同一段 SQL，差别只有事务边界：page 的 MarkStaleForI18n 要把
// pages 的标记与这里的实例标记放进**同一个事务**（同库跨模块的写必须同进同出，AGENTS.md
// 「写操作的事务与回滚」）—— 否则会停在「页面已标、实例未标」的半截状态，而实例这一侧
// 没有任何自动补的入口（要等下一次词条保存才偶然收敛），表现为「改了译文，商品详情页
// 仍是旧字节」且日志里什么都没有。作用域仍在这里设：set_config(..., is_local => true)
// 在事务内可重复设置，外层设过也不冲突（见 pkg/rls.ScopeTx 的分工）。
func (m *Model) MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (n int64, err error) {
	if projectID == "" {
		return 0, errors.New("project id is required")
	}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return 0, serr
	}
	return m.markStaleForI18nIn(ctx, tx, projectID, at)
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
func (m *Model) DeleteInstance(ctx context.Context, projectID, id string) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.deleteInstanceRows(tx, projectID, id)
	})
}

// DeleteInstanceTx 在**调用方已开启的事务**内删除实例及其聚合内从属行
// （语句与错误语义逐条同 DeleteInstance，只是不做事务边界）。
//
// 为什么需要它：调用方（presentation 的 Delete）还要在同一事务里清理 publication 的
// page_routes 行 —— 跨模块 DB 写必须 tx 透传（AGENTS.md「写操作的事务与回滚」，
// 补偿只允许用于跨库/外部系统）。走 DeleteInstance（经 rls.InProjectScope）会
// 另开事务、另取一条连接：外层那条 page_routes 删行在本事务里看不见，原子性也没了。
// 这里只用 rls.ScopeTx 把工程作用域设进**调用方的事务**，不碰事务边界。
func (m *Model) DeleteInstanceTx(tx *gorm.DB, projectID, id string) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return m.deleteInstanceRows(tx, projectID, id)
}

// deleteInstanceRows 实例级联删除的全部语句（事务 / 非事务两条入口共用一份，
// 顺序与每一步的理由见 DeleteInstance 的注释）。
func (m *Model) deleteInstanceRows(tx *gorm.DB, projectID, id string) error {
	if err := tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
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
	return tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&InstanceEntity{}).Error
}

// CreateSnapshot 写快照。
func (m *Model) CreateSnapshot(ctx context.Context, e *SnapshotEntity) error {
	return m.SnapshotDB(ctx).Create(e).Error
}

// NextArtifactVersion 取该实例下一个产物版本号（version 在实例内唯一）。

// GetSnapshot 按 ID 读取快照（可编辑底稿来源，docs/04-C-instance-override.md）。
// document_snapshots 无 project_id 列（属主是实例），不走工程作用域。
func (m *Model) GetSnapshot(ctx context.Context, id string) (e *SnapshotEntity, err error) {
	var row SnapshotEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
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
// ListArtifactHashes 列出本模块认领的全部产物 hash（IDX-015 反向对账）。
// 不过滤状态：元数据行在即视为有人认领，能否回收由 GC 决定。
func (m *Model) ListArtifactHashes(ctx context.Context) (hashes []string, err error) {
	hashes = []string{}
	err = m.db.WithContext(ctx).Raw(
		"SELECT DISTINCT artifact_hash FROM presentation_artifacts WHERE artifact_hash <> ''",
	).Scan(&hashes).Error
	return hashes, err
}

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
//
// 作用域用 rls.ScopeTx 设进**调用方的事务**（不是另开事务）：这些 *Tx 变体本来就是
// service 编排多表原子写入时传进来的 tx，另开事务会让外层未提交数据不可见、同表写入自锁。
func (m *Model) UpdateInstancePointersTx(tx *gorm.DB, projectID string, e *InstanceEntity) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", e.ID, projectID).Updates(map[string]any{
		"current_snapshot_id": e.CurrentSnapshotID,
		"staged_snapshot_id":  e.StagedSnapshotID,
		"staged_artifact_id":  e.StagedArtifactID,
		"active_artifact_id":  e.ActiveArtifactID,
		"stale":               e.Stale,
		"published_at":        e.PublishedAt,
		"update_time":         e.UpdatedAt,
	}).Error
}

// UpdateInstanceTemplateTx 事务内改写实例绑定的模板（issue #14：「详情页模板可选」）。
//
// template_id 曾是只读身份列（见 UpdateInstancePointers 注释），本票起它是**可切换的绑定**：
// 切换必须与本次重建的快照/产物/指针在同一事务里落库，否则会出现「产物来自新模板、
// 实例仍记着旧模板」的漂移，下一次重建又会退回旧模板。
func (m *Model) UpdateInstanceTemplateTx(tx *gorm.DB, projectID, id, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"template_id": templateID,
		"update_time": at,
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
func (m *Model) UpdateInstanceURLTx(tx *gorm.DB, projectID, id, urlPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"url_path":    urlPath,
		"update_time": at,
	}).Error
}

// UpdateInstanceOverrideTx 事务内写入实例级文档覆盖（迁移 281，docs/04-C-instance-override.md）。
//
// 与快照/产物/指针同一事务落库：产物已按覆盖文档编译、实例却还记着「跟随模板」，
// 下次重建就会静默退回模板文档 —— 自定义凭空消失。document 必须非空；清空走
// ClearInstanceOverrideTx（语义不同：那是放弃自定义，不是保存）。
func (m *Model) UpdateInstanceOverrideTx(tx *gorm.DB, projectID, id string, document json.RawMessage, at time.Time) error {
	if len(document) == 0 {
		return errors.New("override document is empty")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"override_document": document,
		"update_time":       at,
	}).Error
}

// ClearInstanceOverrideTx 事务内清除实例级文档覆盖（放弃自定义 / 换模板底稿后的重新同步）。
//
// templateID 非空时同事务切换 template_id（换底稿 = 放弃自定义，两件事一体成型），
// 调用方随后以新文档重建；只清不切用于「模板有新版，放弃自定义跟进新版」。
func (m *Model) ClearInstanceOverrideTx(tx *gorm.DB, projectID, id, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	updates := map[string]any{
		"override_document": nil,
		"update_time":       at,
	}
	if templateID != "" {
		updates["template_id"] = templateID
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(updates).Error
}

// FindInstanceByPath 查同工程内占用该路径的其他展示实例（改 URL 的占用预检）。
//
// 为什么必须查本表而不只查 page_routes：详情页实例的 URL 占用登记进 page_routes
// 是本次才补上的，历史实例在路由表里没有行；以本表为真源，预检不依赖路由登记的
// 完整性（漏登只影响「别人防我」，不影响「我防别人」）。返回 ErrRecordNotFound
// 表示路径空闲。
func (m *Model) FindInstanceByPath(ctx context.Context, projectID, urlPath, excludeInstanceID string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND url_path = ? AND deleted_at IS NULL", projectID, urlPath)
		if excludeInstanceID != "" {
			q = q.Where("id <> ?", excludeInstanceID)
		}
		return q.First(&row).Error
	})
	if err != nil {
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
// projectID 是必填的工程作用域（DB-009 第二批）：本方法是**单工程**版本，
// 跨工程扇出由 service 枚举工程后逐个调用 —— presentation_instances 带 FORCE 策略，
// 无作用域时这条 UPDATE 会静默匹配 0 行（依赖失效不再触发自动重建，日志上却一切正常）。
func (m *Model) MarkStaleByDependency(ctx context.Context, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(`
		WITH affected AS (
			SELECT DISTINCT d.presentation_id AS presentation_id
			FROM presentation_dependencies d
			JOIN presentation_instances p ON p.id = d.presentation_id
			WHERE d.dependency_kind = ?
			  AND d.dependency_key = ?
			  AND p.deleted_at IS NULL
			  AND (d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM presentation_publications WHERE presentation_id = p.id))
		)
		UPDATE presentation_instances SET stale = true, update_time = ?
		WHERE deleted_at IS NULL AND project_id = ? AND id IN (SELECT presentation_id FROM affected)
		RETURNING id`, kind, key, at, projectID).Scan(&ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// BlockDocRefRow 文档树里引用了目标块的自动发布实例（覆盖文档或快照）。
type BlockDocRefRow struct {
	InstanceID   string `gorm:"column:instance_id"`
	URLPath      string `gorm:"column:url_path"`
	EntityType   string `gorm:"column:entity_type"`
	EntityID     string `gorm:"column:entity_id"`
	FromSnapshot bool   `gorm:"column:from_snapshot"`
}

// ListBlockDocumentRefs 列出**本工程内**文档树引用了 blockID 的自动发布实例（审计 ARCH-02）。
//
// 两个来源都要查，因为它们回答的是不同的事实：
//   - override_document：实例级文档覆盖（迁移 281 的「独立文档模式」，render_mode=document）——
//     这份文档不在任何模板里，模板侧与页面侧的扫描都看不见它；
//   - document_snapshots.document：模板派生出的快照。快照是**可编辑源码的派生输入**
//     （不是编译产物），删块后重建该实例就会缺一段 —— 所以它阻断删除；
//     真正不阻断的是 presentation_artifacts 里那些不可变产物字节（由 GC 策略处理）。
//
// document_snapshots 没有 project_id 列（属主是实例），作用域由 JOIN 的实例行承担。
func (m *Model) ListBlockDocumentRefs(ctx context.Context, projectID, blockID string) (rows []BlockDocRefRow, err error) {
	if projectID == "" {
		return nil, errors.New("project id is required")
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(`
			SELECT i.id::text AS instance_id, i.url_path, i.entity_type, i.entity_id, false AS from_snapshot
			FROM presentation_instances i
			WHERE i.project_id = ? AND i.deleted_at IS NULL
			  AND jsonb_path_query_array(i.override_document, '$.**.blockId') @> jsonb_build_array(?::text)
			UNION ALL
			SELECT i.id::text, i.url_path, i.entity_type, i.entity_id, true
			FROM presentation_instances i
			JOIN document_snapshots s ON s.presentation_instance_id = i.id
			WHERE i.project_id = ? AND i.deleted_at IS NULL
			  AND jsonb_path_query_array(s.document, '$.**.blockId') @> jsonb_build_array(?::text)
			ORDER BY 2 ASC, 1 ASC, 5 ASC`,
			projectID, blockID, projectID, blockID,
		).Scan(&rows).Error
	})
	return rows, err
}
