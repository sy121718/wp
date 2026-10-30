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
	"cmp"
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	// OverrideDocument 实例级文档覆盖（迁移 281，AGENTS.md）。
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
	// InputVersion 编译输入代次（迁移 699）：**输入变更**（草稿保存 / 译文扇出 / 依赖标记）
	// 与那次变更在同一个事务里原子 +1；**激活不推进**，只按它做谓词
	// （WHERE input_version = $expect）证明「我编译的是最新输入」。
	//
	// 为什么不是「提交顺序号」：提交顺序只说明谁后提交，不说明谁编译的是新输入 ——
	// 旧输入晚到照样拿到更大的号并覆盖新产物。激活只验证、不推进，也让同一批多语言
	// 发布里每种语言读到的都是同一个值（不会因第一种语言成功而让其余语言失败）。
	//
	// 与 page 侧 PageEntity.InputVersion 同一语义、同一迁移；与 build_jobs.input_version
	// （**任务序列号**）语义独立，双向禁止赋值（迁移 699 的注释与规划草案 §2.6）。
	InputVersion int64 `gorm:"column:input_version;not null"`
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
//
// 清单只有一个真源（审计 DB-02，迁移 305）：本表曾有 build_input_manifest 与
// manifest 两列，recordArtifactTx 把同一个 manifestJSON 平铺写进两列 —— 同字节、
// 零读取者（全仓只有 manifest 被读：artifact.toResp 取 canonicalPath、
// page.persistDependenciesFromManifest 反序列化依赖）。判定为同义后合并为 manifest
// 一列，305 已把重复列删掉。
//
// 为什么不保留「输入清单」列：代码里不存在这样的产物 —— pipeline.BuildInput 是
// 内存结构（PageID/Lang/Path/DocJSON/Usage/Diagnostics），从不序列化落库；输入侧的
// 事实由 document_snapshots.document（快照）与 source_hash / build_input_hash
// 各自承载。也就是说 BuildInputManifest 从来没装过「输入」。
//
// 注意：这份 Manifest 是**输出清单**，参与产物 hash（sha256(manifestJSON + indexHTML)），
// 删列只动存储侧的重复副本，不改它的 JSON 内容。
type ArtifactEntity struct {
	ID                     string `gorm:"column:id;primaryKey"`
	PresentationInstanceID string `gorm:"column:presentation_instance_id;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:1"`
	SnapshotID             string `gorm:"column:snapshot_id;not null"`
	Version                int64  `gorm:"column:version;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:2"`
	// Lang 构建语言（I18N-013）：同版本多语言各占一行。
	Lang             string          `gorm:"column:lang;not null;uniqueIndex:uk_presentation_artifacts_instance_version_lang,priority:3"`
	SourceHash       string          `gorm:"column:source_hash;not null"`
	BuildInputHash   string          `gorm:"column:build_input_hash;not null"`
	ArtifactProvider string          `gorm:"column:artifact_provider;not null"`
	ArtifactKey      string          `gorm:"column:artifact_key;not null"`
	ArtifactHash     string          `gorm:"column:artifact_hash;not null"`
	CompilerVersion  string          `gorm:"column:compiler_version;not null"`
	RegistryVersion  string          `gorm:"column:registry_version;not null"`
	Manifest         json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState     string          `gorm:"column:payload_state;not null"`
	PayloadDeletedAt *time.Time      `gorm:"column:payload_deleted_at"`
	Note             string          `gorm:"column:note;not null"`
	CreatedBy        string          `gorm:"column:created_by;not null"`
	CreatedAt        time.Time       `gorm:"column:create_time;not null"`
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

// ErrCommitUncertain 回调已跑完、但提交本身失败（提交结果不明）。
//
// 与"回调里出错 → 明确回滚"必须区分：后者可以断言"业务没发生"，前者不能 ——
// 连接在 COMMIT 阶段断开时数据库可能已经提交。把它当回滚处理会 Abort 掉一条
// 其实已经生效的业务意图，撤链永远不再重放（线上残留死链接）。
var ErrCommitUncertain = errors.New("presentation: 事务提交结果不明")

// InTransaction 带"回调是否跑完"事实的事务封装。
//
// 判据只有一条：回调返回 error → 明确回滚（callbackCompleted=false）；
// 回调返回 nil 但 Transaction 仍然报错 → **提交不明**（uncertain=true），
// 错误一律包成 ErrCommitUncertain，让调用方按"可能已提交"处理。
//
// callbackCompleted 由**包装器自己的闭包**置位（而不是靠闭包外的标志 + 事后猜测）：
// gorm 只在回调返回 nil 时才执行 COMMIT —— 这一点是分流的唯一依据。
func (m *Model) InTransaction(ctx context.Context, fn func(tx *gorm.DB) error) (callbackCompleted, uncertain bool, err error) {
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rerr := fn(tx)
		if rerr == nil {
			callbackCompleted = true
		}
		return rerr
	})
	if err != nil && callbackCompleted {
		return true, true, fmt.Errorf("%w: %v", ErrCommitUncertain, err)
	}
	return callbackCompleted, false, err
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

// payloadStateAvailable 产物负载可用（与 page_artifacts / presentation_artifacts 的
// CHECK 取值同口径）。只用于「当前产物」判定：已标记回收的行不构成重建理由。
const payloadStateAvailable = "available"

// ListStaleInstanceIDsByRegistryVersion 在**调用方给定的当前产物集合**内挑出
// registry_version 与 current 不同的产物，返回它们所属的实例 ID（去重、字典序）。
//
// 与 page 侧经 artifact 契约的同名比对同义（见 artifactmodel.ListStalePageIDs 的注释）：
// 集合由调用方从本模块语言账本（presentation_publications，见 ListCurrentArtifactIDs）
// 里选出，本方法只回答「这些行里哪些是旧组件产出的」。
//
// id = ANY(string_to_array(?, ',')::uuid[]) 而不是 id IN ?：组件升级时入参可能上万，
// IN ? 会把数组展开成同数量的绑定参数、逼近 PostgreSQL 的 65535 上限（超限即整批报错，
// 结果是「组件更新后一个实例都标不上」）。实例 id 是 uuid（不含逗号），分隔安全。
func (m *Model) ListStaleInstanceIDsByRegistryVersion(ctx context.Context, current string, artifactIDs []string) (ids []string, err error) {
	if len(artifactIDs) == 0 {
		return nil, nil
	}
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("payload_state = ? AND registry_version <> ? AND id = ANY(string_to_array(?, ',')::uuid[])",
			payloadStateAvailable, current, strings.Join(artifactIDs, ",")).
		Distinct().Order("presentation_instance_id").Pluck("presentation_instance_id", &ids).Error
	return ids, err
}

// ListStaleInstanceIDs 列出**本工程**内待重建（stale = true）且未删除的实例 id。
//
// 排序是 (update_time, id) 升序：menu_stale 的同步重建按这个顺序逐条执行，最老的
// 变更先收敛；给确定的顺序是为了让「同一批 stale 数据两次调用拿到同一序列」，
// 否则失败边界会随查询计划漂移（哪一条先失败取决于块返回顺序）。
//
// 工程维度必填而不是可选（与 ListInstances 同一理由）：presentation_instances 带
// FORCE 策略，无作用域的读在非超级角色下是空集（fail closed），而「列表空」会让
// 「一个实例都没重建」看起来像「本来就没事」。
func (m *Model) ListStaleInstanceIDs(ctx context.Context, projectID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("project id is required")
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND stale", projectID).
			Order("update_time ASC, id ASC").
			Pluck("id", &ids).Error
	})
	return ids, err
}

// MarkStaleByIDs 在**指定工程作用域内**按实例 ID 列表标记待重建，返回本次真正命中的实例 ID。
//
// 与 MarkStale 的差别是返回值：本方法用 RETURNING id 回读真实命中的行，而不是回带入参 ——
// 组件升级时入参来自产物元数据，可能横跨多个工程、可能混着已删实例，回显会让
// 「日志说标了 N 个、实际只有 M 个」永远查不出来（与 page 侧 MarkStaleByIDs 同形）。
//
// projectID 必填（DB-009）：presentation_instances 带 FORCE 策略，缺作用域时这条 UPDATE
// 在非超级角色下静默匹配 0 行 —— 表现是「组件换了，详情页却从没被标过」且没有任何报错。
func (m *Model) MarkStaleByIDs(ctx context.Context, projectID string, ids []string, at time.Time) (marked []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("project id is required")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		// 多行 RETURNING 的唯一可用形态：Model 的目标是**切片**，GORM 把 RETURNING 结果扫进去。
		// （Updates 之后再 .Scan(&marked) 会另发一条 SELECT —— 实测报
		// `Scan error on column index 0, name "id": destination not a pointer`，而语句文本是
		// `SELECT * FROM "presentation_instances" ...`：UPDATE 的结果根本没被读。）
		return tx.Model(&rows).
			Where("project_id = ? AND deleted_at IS NULL AND id IN ?", projectID, ids).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(at)).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		marked = append(marked, r.ID)
	}
	return marked, nil
}

// staleMarkUpdates 「把实例标记为待重建」的唯一写法：stale=true 与 input_version 原子 +1。
//
// 为什么两者必须同一条语句、且写成表达式（input_version + 1）而不是读改写：
//   - 标记 stale 恰恰就是「输入变了、产物过期」这件事本身。只标 stale 不推进版本会留下
//     「已过期但版本没变」的行 —— 那次变更编译出来的产物一旦晚到，激活谓词
//     （input_version = $expect）会放行它，旧字节覆盖新字节（与 page 侧同名助手同理由）；
//   - 读改写要先 SELECT 再 UPDATE，两条语句之间别的入口的 +1 会被覆盖掉（丢版本）；
//     gorm.Expr 让 +1 在数据库里原子完成，并发标记互不覆盖。
//
// 为什么抽成一个函数：本模块有四个标记入口（按 id / 按依赖 / 按模板依赖 / 整站译文），
// 各拼一份 map 时新加入口漏掉 input_version 不会有任何编译错误。
func staleMarkUpdates(at time.Time) map[string]any {
	return map[string]any{
		"stale":         true,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}
}

// MarkStale 批量标记实例待重建（依赖失效后的落库动作）。
func (m *Model) MarkStale(ctx context.Context, projectID string, ids []string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res := tx.Model(&InstanceEntity{}).
			Where("project_id = ? AND id IN ?", projectID, ids).
			Updates(staleMarkUpdates(at))
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
		Updates(staleMarkUpdates(at))
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

// GetSnapshot 按 ID 读取快照（可编辑底稿来源，AGENTS.md）。
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
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("artifact_hash <> ''").
		Distinct().
		Pluck("artifact_hash", &hashes).Error
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

// GetArtifact 按产物 ID 查询（详情口径：带 manifest 全量）。
func (m *Model) GetArtifact(ctx context.Context, id string) (e *ArtifactEntity, err error) {
	var row ArtifactEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetArtifactHash 只取某产物行的 artifact_hash（轻量 projection）。
//
// 为什么单开一个方法而不是让调用方接着用 GetArtifact：实例列表 / 回执核对这类路径
// 只要一个哈希，GetArtifact 会把整行（含 manifest 这个 JSONB）拽出来 —— 列表按行数
// 放大，而 manifest 是产物里最大的那一列。列白名单写死在这里，调用方拿不到多余的列，
// 「列表别带大字段」就不依赖每个调用点自律。
//
// 未找到返回 gorm.ErrRecordNotFound（与 GetArtifact 同语义），由调用方决定是当
// 「没有产物」还是当错误。
func (m *Model) GetArtifactHash(ctx context.Context, id string) (hash string, err error) {
	if strings.TrimSpace(id) == "" {
		return "", gorm.ErrRecordNotFound
	}
	err = m.db.WithContext(ctx).Model(&ArtifactEntity{}).
		Where("id = ?", id).Select("artifact_hash").Scan(&hash).Error
	if err != nil {
		return "", err
	}
	if hash == "" {
		// Scan 到零值：id 不存在（或该行 artifact_hash 为空串，生产 DDL 为 NOT NULL）。
		return "", gorm.ErrRecordNotFound
	}
	return hash, nil
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
		"template_id":   templateID,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}).Error
}

// RecordBatchTemplateTx / RecordBatchURLTx 记录**本次发布批次自己编译出来的**绑定与路径。
//
// 与 UpdateInstanceTemplateTx / UpdateInstanceURLTx 的差别只有一处：**不推进
// input_version**。这两个方法的调用方是 persistMultiLangArtifacts —— 它在同一批事务里
// 先落快照/产物、再把这批产物依据的 template_id / url_path 记下来。那不是「输入变了」，
// 而是「我按这份输入编译完了」：批次进入第二阶段还要用冻结时的版本做谓词
// （WHERE input_version = $expect）证明没有更新的输入插进来，若这里顺手 +1，
// 谓词会被自己推进掉 —— 表现为每次发布都判定「输入已变」而失败，或者更糟：
// 把并发的真实推进掩盖成「我自己推的」。
//
// 反过来，用户可见的绑定写入（ReapplyPreset / 保存独立文档 / 改 URL）必须走推进版本
// 的那两个方法：那是真正的输入变化，在途的旧批次必须因此失效。
func (m *Model) RecordBatchTemplateTx(tx *gorm.DB, projectID, id, templateID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"template_id": templateID,
		"update_time": at,
	}).Error
}

// RecordBatchURLTx 见 RecordBatchTemplateTx（同一条理由：记录批次输出，不是输入变化）。
func (m *Model) RecordBatchURLTx(tx *gorm.DB, projectID, id, urlPath string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
		"url_path":    urlPath,
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
		"url_path":      urlPath,
		"input_version": gorm.Expr("input_version + 1"),
		"update_time":   at,
	}).Error
}

// UpdateInstanceOverrideTx 事务内写入实例级文档覆盖（迁移 281，AGENTS.md）。
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
		"input_version":     gorm.Expr("input_version + 1"),
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
		"input_version":     gorm.Expr("input_version + 1"),
		"update_time":       at,
	}
	if templateID != "" {
		updates["template_id"] = templateID
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(updates).Error
}

// PersistPublishInputURLTx 事务内**持久化一次改 URL 输入意图**（url_path + input_version + 1）。
//
// 与 RecordBatchURLTx 的分界线（这是"输入意图线性化"的唯一实现）：
//   - RecordBatchURLTx 记的是「我这个批次按这份路径编译完了」，**输出**，不动版本；
//   - 本方法记的是「用户说要改成这个路径」，**输入**，必须推进版本 —
//     否则两个并发改 URL（A→/x、B→/y）各自冻结、各自提交都能成功，后者静默覆盖前者，
//     谁都不会收到冲突（输入意图根本没有被记录过一次）。
//
// 时机：在**冻结段**（持实例锁）内先落意图，再拿返回的新版本作为本批次的 expect。
// 同一次提交里既推进又作为自身谓词基准，因此不会自拒；后到的并发批次在行锁内
// 看到 locked.InputVersion != frozen.inputVersion 即判冲突（errInputVersionMoved）。
//
// 落库即意图，编译失败也**不回滚**：控制面 url_path 已经是新值、旧产物仍在线
// （指针没动），实例被标 stale 等待下一次重建 —— 这是"输入已变、产物待追"的正常形态，
// 不是回滚。调用方不得把它描述成"修改失败、什么都没变"。
//
// 返回推进后的新版本号（UPDATE 用 RETURNING 回读，不推算）。
func (m *Model) PersistPublishInputURLTx(tx *gorm.DB, projectID, id, urlPath string, at time.Time) (int64, error) {
	if strings.TrimSpace(urlPath) == "" {
		return 0, errors.New("url path is required")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	var rows []struct{ InputVersion int64 }
	err := tx.Model(&rows).
		Where("id = ? AND project_id = ?", id, projectID).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "input_version"}}}).
		Updates(map[string]any{
			"url_path":      urlPath,
			"input_version": gorm.Expr("input_version + 1"),
			// stale 与意图**同一条语句**置位：新路径还没有产物（编译在锁外），
			// 实例此刻确实"线上内容与输入不符"。不能"随后再标" —— 那之间存在一个窗口，
			// 该行看起来既是新输入又有在线产物，任何读侧都会把它当干净的；
			// 而崩在这个窗口里就再没有人会去标它（意图已落库，没有回执再提示它）。
			"stale":       true,
			"update_time": at,
		}).Error
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, gorm.ErrRecordNotFound
	}
	return rows[0].InputVersion, nil
}

// MarkStaleWithoutVersion 只标 stale、**不推进** input_version（持久补偿专用）。
//
// 与 MarkStale（staleMarkUpdates：stale + version+1）的区别是语义而非便利：
//   - MarkStale 用于「输入变了，产物过期」—— 版本必须动，在途批次要因此失效；
//   - 本方法用于「线上与库已经一致了，只是这份产物本身需要重做」—— 回执 superseded
//     收敛就属于这一类：输入没变（版本已被别的 attempt 推过），在这里再 +1 会把正在跑的
//     发布无故判过期，而它们编译的输入未必有问题。
func (m *Model) MarkStaleWithoutVersion(ctx context.Context, projectID string, ids []string, at time.Time) (n int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(projectID) == "" {
		return 0, errors.New("project id is required")
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

// MarkInstanceMenuStaleTx 在**调用方的事务**内，把「活跃或暂存产物声明了该菜单位置依赖」
// 的实例标记为待重建，返回真正命中的行数（RETURNING 回读，不是入参回显）。
//
// 命中口径与 MarkStaleByDependency 逐字一致（presentationDepScope + 三个指针），差别是
// 按菜单依赖键（pipeline.MenuKey = menu:{projectID}:{kind}）定位，调用方只给 kind。
//
// 版本语义与其它标记入口**相同**：菜单译文烘进 HTML 字节，是实体的编译输入，所以走
// staleMarkUpdates（stale=true 与 input_version 原子 +1）。只标 stale 会让「旧输入编译
// 的产物」在激活谓词 input_version = $expect 处仍然通过，随后把 stale 清回 false。
// 导航侧 build_jobs.input_version 是任务序列号，与本列无关，不参与、不赋值。
//
// 事务与作用域：tx 由调用方给（导航把「译文 UPSERT + 页标记 + 实例标记 + 队列入队」放进
// 同一个事务），这里只用 rls.ScopeTx 把工程作用域设进那个事务。
//
// kind 白名单在此校验：未知种类直接报错，不静默把全站实例标脏。
func (m *Model) MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, errors.New("project id is required")
	}
	if !IsMenuStaleKind(kind) {
		return 0, fmt.Errorf("unknown menu kind %q", kind)
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	// 依赖键由调用方给的是 kind，键的构造只在这里出现一份。
	key := "menu:" + projectID + ":" + kind
	var rows []InstanceEntity
	if err := tx.WithContext(ctx).Model(&rows).
		Where("deleted_at IS NULL AND project_id = ?", projectID).
		Where("id IN (?)", presentationDepScope(tx, depKindMenu, key)).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Updates(staleMarkUpdates(at)).Error; err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

// MarkInstanceNavigationStaleTx 在**调用方的事务**内，把「活跃或暂存产物声明了
// **按具体菜单项**（navigation:{itemID}）依赖」的实例标记为待重建，返回命中行数。
//
// 为什么必须有这一条：渲染期记录两种导航消费 —— 按位置记 menu:{project}:{kind}、
// 按项记 navigation:{itemID}（builder/core/render.go 的 UseMenu / UseNavigation）。
// 只处理前者时，按项引用的实例既不会被标 stale（产物停在旧菜单），也没有版本屏障
// （旧输入编译的产物在激活谓词处照旧通过，随后把 stale 清回 false）。
//
// 命中口径与 MarkInstanceMenuStaleTx 逐字一致（presentationDepScope + staleMarkUpdates）：
// 唯一差别是依赖种类与键的构造 —— 按项键用全局唯一 UUID，不带工程分量
// （见 pipeline.NavigationKey），所以键不能再拼 projectID；工程作用域仍由
// Where project_id 与 rls.ScopeTx 保证。
//
// 版本语义相同：菜单项内容烘进 HTML 字节，是编译输入 → stale=true 与 input_version 原子 +1。
// 导航侧 build_jobs.input_version 是任务序列号，与本列无关，不参与、不赋值。
func (m *Model) MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string, at time.Time) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, errors.New("project id is required")
	}
	if strings.TrimSpace(navigationID) == "" {
		return 0, errors.New("navigation id is required")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	key := depKindNavigation + ":" + navigationID
	var rows []InstanceEntity
	if err := tx.WithContext(ctx).Model(&rows).
		Where("deleted_at IS NULL AND project_id = ?", projectID).
		Where("id IN (?)", presentationDepScope(tx, depKindNavigation, key)).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Updates(staleMarkUpdates(at)).Error; err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

// depKindNavigation 按项导航依赖的种类名，与 pipeline.DepKindNavigation 逐字一致
// （同 depKindMenu 的理由：model 只做本模块表访问，常量在 model 单测里交叉断言）。
const depKindNavigation = "navigation"

// depKindMenu 菜单依赖的种类名，与 pipeline.DepKindMenu 逐字一致。
//
// 为什么不 import pipeline：presentation/model 能 import pipeline（服务层已经在用），
// 但 model 只做本模块表访问，依赖种类是**构建口径**而不是表结构 —— 复制一个字面量
// 会让「pipeline 改名、这边静默 0 行」。所以这里只放常量，并在 model 单测里用
// pipeline.DepKindMenu 交叉断言（见 presentation_menu_stale_test.go），改名时测试先红。
const depKindMenu = "menu"

// 菜单类型：与导航 / build 两侧的 menuKinds 逐字一致（四个会进 HTML 的位置）。
const (
	MenuStaleKindHeader       = "header"
	MenuStaleKindHeaderMobile = "header_mobile"
	MenuStaleKindFooter       = "footer"
	MenuStaleKindFooterMobile = "footer_mobile"
)

// IsMenuStaleKind 是否是本模块认识的菜单种类（未知种类直接报错，不静默全站标记）。
func IsMenuStaleKind(kind string) bool {
	switch kind {
	case MenuStaleKindHeader, MenuStaleKindHeaderMobile, MenuStaleKindFooter, MenuStaleKindFooterMobile:
		return true
	}
	return false
}

// LockInstanceForActivationTx 在**调用方的事务**内锁定实例行并回读当前事实（纯锁读，不写）。
//
// 这是规划草案 §2.3 的 P2：「SELECT … FOR UPDATE 锁实体行 + 复验版本」。它只做两件事：
// 把行锁住（此后并发的激活/标记在本事务提交前排队），把锁内的**权威值**读出来
// （尤其是 input_version）。写一律交给 WriteInstanceActivationTx —— 锁读与写分开，
// 是为了让「复验 → 外部副作用（FS 激活）→ 落库」这段顺序在调用方眼里是显式的三步，
// 也让 model 不必知道 FS 的存在（model 纯 DB，规划草案 §2.3 的硬约束）。
//
// 为什么用 First + id 等值谓词而不是 MarkStaleByIDs 那种 ANY(数组)：这里锁的是**一行**，
// 单行等值能走主键索引、锁粒度最小；数组形态只在批量标记时需要（参数规模问题）。
//
// 返回 gorm.ErrRecordNotFound 表示实例已不存在（或不在本工程作用域内）。
func (m *Model) LockInstanceForActivationTx(ctx context.Context, tx *gorm.DB, projectID, id string) (e *InstanceEntity, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	var row InstanceEntity
	if err = tx.WithContext(ctx).Model(&InstanceEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", id, projectID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ErrInputVersionMoved 激活谓词未命中：锁内读到的 input_version 与冻结时捕获的不等。
//
// 这是一个**哨兵**（不是对外文案）：service 据此判定「本次批次编译的输入已过期」，
// 丢弃本次产物、重试或放弃，绝不把旧字节激活上去。对外错误由 service 换成
// presentationenums.ErrInputMoved（409）。
var ErrInputVersionMoved = errors.New("presentation input version moved")

// WriteInstanceActivationTx 在**调用方的事务**内落下激活结果，并以 input_version 作谓词。
//
// 与 UpdateInstancePointersTx 的差别只有一处：WHERE 里多了 input_version = ?，
// 且**不推进**它（激活不是输入变化，规划草案 §2.2）。0 行命中返回 ErrInputVersionMoved。
//
// 为什么谓词不能省成「锁内 SELECT 过就不用再判」：锁内 SELECT 与 UPDATE 之间确实没有
// 别的写入者能插进来（行锁在），但**同一事务内**前面的语句可能已经推进过版本
// （例如批次记录路径误用了推进变体）—— 谓词是最后一道自检，把「我用旧版本编译、
// 却把它当新版本落下」变成一条明确的错误而不是一次静默覆盖。
func (m *Model) WriteInstanceActivationTx(tx *gorm.DB, projectID string, e *InstanceEntity, expectInputVersion int64) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	res := tx.Model(&InstanceEntity{}).
		Where("id = ? AND project_id = ? AND input_version = ?", e.ID, projectID, expectInputVersion).
		Updates(map[string]any{
			"current_snapshot_id": e.CurrentSnapshotID,
			"staged_snapshot_id":  e.StagedSnapshotID,
			"staged_artifact_id":  e.StagedArtifactID,
			"active_artifact_id":  e.ActiveArtifactID,
			"stale":               e.Stale,
			"published_at":        e.PublishedAt,
			"update_time":         e.UpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInputVersionMoved
	}
	return nil
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

// presentationDepScope 依赖表里命中 (kind,key) 且实例未删除的实例 id 集合（去重）。
//
// 原实现是一条 `WITH affected AS (…) UPDATE … WHERE id IN (SELECT … FROM affected)`：
// GORM 无 CTE API，改成行内子查询 —— 两段各自只承担「筛出实例 id」这一件事，
// 外层 UPDATE 的原子性不受影响。
//
// 两个调用方的差异只有一个条件：模板模式的标记要额外排除「文档模式下的正文模板」
// （detail 页脱离自身模板后，改正文模板不该再标它），所以那个条件由调用方追加。
func presentationDepScope(tx *gorm.DB, kind, key string) *gorm.DB {
	return tx.Table(tableNamePresentationDependencies+" AS d").
		Select("DISTINCT d.presentation_id").
		Joins("JOIN "+tableNamePresentationInstances+" p ON p.id = d.presentation_id").
		Where("d.dependency_kind = ? AND d.dependency_key = ? AND p.deleted_at IS NULL", kind, key)
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
	var rows []InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		affected := presentationDepScope(tx, kind, key).
			Where("(d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM " + tableNamePresentationPublications + " WHERE presentation_id = p.id))")
		return tx.Model(&rows).
			Where("deleted_at IS NULL AND project_id = ?", projectID).
			Where("id IN (?)", affected).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(at)).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ids = append(ids, r.ID)
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
		// 两段各自查询、Go 侧 UNION ALL 合并：GORM v1.31.1 无 UNION API，而 UNION ALL
		// 在这两段之间**不去重**（同一实例既可能命中覆盖文档、也可能命中快照，两行都该返回）。
		var fromOverride []BlockDocRefRow
		if qerr := tx.Model(&InstanceEntity{}).
			Select("id::text AS instance_id, url_path, entity_type, entity_id, false AS from_snapshot").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Where("jsonb_path_query_array(override_document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Scan(&fromOverride).Error; qerr != nil {
			return qerr
		}
		var fromSnapshot []BlockDocRefRow
		if qerr := tx.Table(tableNameDocumentSnapshots+" AS s").
			Select("i.id::text AS instance_id, i.url_path, i.entity_type, i.entity_id, true AS from_snapshot").
			Joins("JOIN "+tableNamePresentationInstances+" i ON i.id = s.presentation_instance_id").
			Where("i.project_id = ? AND i.deleted_at IS NULL", projectID).
			Where("jsonb_path_query_array(s.document, '$.**.blockId') @> jsonb_build_array(?::text)", blockID).
			Scan(&fromSnapshot).Error; qerr != nil {
			return qerr
		}
		rows = append(fromOverride, fromSnapshot...)
		// 原 SQL 的 ORDER BY 2 ASC, 1 ASC, 5 ASC → url_path, instance_id, from_snapshot（false 在前）。
		slices.SortStableFunc(rows, func(a, b BlockDocRefRow) int {
			if c := cmp.Compare(a.URLPath, b.URLPath); c != 0 {
				return c
			}
			if c := cmp.Compare(a.InstanceID, b.InstanceID); c != 0 {
				return c
			}
			if a.FromSnapshot == b.FromSnapshot {
				return 0
			}
			if !a.FromSnapshot {
				return -1
			}
			return 1
		})
		return nil
	})
	return rows, err
}

// LockPresentationEntityAdvisoryTx 在**调用方事务**内取"presentation:<实例 id>"的
// 事务级顾问锁（pg_advisory_xact_lock，提交/回滚自动释放）。
//
// 用途唯一：实例行已经被物理删除（DeleteInstanceTx 是真 DELETE），此时没有行可锁，
// 而"撤掉它遗留的访问面链接"仍必须与"正在进行的那次删除"互斥。删除路径在同一个
// 事务里也会取这把锁，所以两侧的时间线不会交错到对方的中间态。
//
// 为什么不是进程内锁：物理删行之后不能退化到只靠进程锁 —— 多实例部署时锁不到同一处，
// 而这里的副作用（撤链）是不可回退的。键取 "presentation:<id>" 与本模块其它锁键同源，
// 不与 product 的 outbox 键冲突（后者是 "类型:实体" 的另一种组合，且已 hashtext 化）。
func (m *Model) LockPresentationEntityAdvisoryTx(ctx context.Context, tx *gorm.DB, instanceID string) error {
	if tx == nil {
		return errors.New("presentation: 实体顾问锁必须在调用方事务内取")
	}
	key := strings.TrimSpace(instanceID)
	if key == "" {
		return ErrInstanceNil
	}
	// pg_advisory_xact_lock 是无表、无 Entity 可映射的数据库函数调用，GORM 没有对应
	// 表达能力（与 internal/module/product/model/product_outbox_model.go 同一先例，
	// 登记在 internal/architecture/raw_sql_boundary_test.go 的 rawSQLAllowedList）。
	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
		"presentation:"+key).Error
}
