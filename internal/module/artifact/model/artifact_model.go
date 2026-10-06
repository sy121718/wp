// Package artifactmodel 实现 artifact 模块 page_artifacts、content_objects
// 与 page_artifact_objects 表持久化：产物元数据投影与内容对象闭包。
package artifactmodel

import (
	"context"
	"encoding/json"
	"strings"
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
//
// 关于 DDL 里那个不在本结构体上的 build_input_manifest 列（审计 DB-02，迁移 305）：
// 它历史上被写入与 manifest 同字节的输出清单（Record / EnsureRecord 两列同取
// req.Manifest），且全仓零读取者。305 把它放开为可空、写入侧不再写它（新行 NULL），
// 存量行的字节原样保留 —— 页侧删除该列要同步 5 个 raw INSERT 的列清单，其中 3 个
// 不在本票文件域，故「保留列 + 停止写入」在本票内收口，删列留待后续迁移。
// 本结构体**故意不映射**该列：没有读取者，映射它只会让每个产物查询多拽一个大字段。
// 注意 manifest 是**输出清单**（参与产物 hash），不是「输入清单」；输入侧的事实由
// source_document 与 source_hash / build_input_hash 承载。
type PageArtifactEntity struct {
	ID                        string          `gorm:"column:id;primaryKey"`
	PageID                    string          `gorm:"column:page_id;not null;uniqueIndex:uk_page_artifacts_page_version_lang"`
	Version                   int64           `gorm:"column:version;not null;uniqueIndex:uk_page_artifacts_page_version_lang"`
	Lang                      string          `gorm:"column:lang;not null;default:'zh-CN';uniqueIndex:uk_page_artifacts_page_version_lang"`
	SourceDocument            json.RawMessage `gorm:"column:source_document;type:jsonb;not null"`
	PageDocumentSchemaVersion int             `gorm:"column:page_document_schema_version;not null"`
	SourceHash                string          `gorm:"column:source_hash;not null"`
	BuildInputHash            string          `gorm:"column:build_input_hash;not null"`
	ArtifactProvider          string          `gorm:"column:artifact_provider;not null"`
	ArtifactKey               string          `gorm:"column:artifact_key;not null"`
	ArtifactHash              string          `gorm:"column:artifact_hash;not null"`
	CompilerVersion           string          `gorm:"column:compiler_version;not null"`
	RegistryVersion           string          `gorm:"column:registry_version;not null"`
	Manifest                  json.RawMessage `gorm:"column:manifest;type:jsonb;not null"`
	PayloadState              string          `gorm:"column:payload_state;not null"`
	PayloadDeletedAt          *time.Time      `gorm:"column:payload_deleted_at"`
	Note                      string          `gorm:"column:note;not null"`
	CreatedBy                 string          `gorm:"column:created_by;not null"`
	CreatedAt                 time.Time       `gorm:"column:create_time;not null"`
}

func (PageArtifactEntity) TableName() string { return tableNamePageArtifacts }

// ContentObjectEntity 对应 content_objects 表：共享内容对象（Locator 投影）。
type ContentObjectEntity struct {
	ContentHash string     `gorm:"column:content_hash;primaryKey"`
	Provider    string     `gorm:"column:provider;not null"`
	ObjectKey   string     `gorm:"column:object_key;not null"`
	ByteSize    int64      `gorm:"column:byte_size;not null"`
	CreatedAt   time.Time  `gorm:"column:create_time;not null"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
}

func (ContentObjectEntity) TableName() string { return tableNameContentObjects }

// PageArtifactObjectEntity 对应 page_artifact_objects 表：产物 → 内容对象闭包。
type PageArtifactObjectEntity struct {
	ArtifactID  string `gorm:"column:artifact_id;primaryKey"`
	ContentHash string `gorm:"column:content_hash;primaryKey"`
}

func (PageArtifactObjectEntity) TableName() string { return tableNamePageArtifactObjects }

// ---- 列表投影（审计 DB-02）：列表 / 管理视图只带白名单列，绝不带大字段 ----
//
// 为什么要有专门的投影类型而不是继续 Find(&[]PageArtifactEntity)：列表查询按行数放大，
// 而 page_artifacts 里最大的三列（source_document 源码快照、manifest 输出清单、
// build_input_manifest 遗留重复列）对列表语义（哪一版 / 哪个语言 / hash / 状态）毫无用处。
// 用完整实体读列表 = 每行多拽几百 KB 到几 MB 的 JSONB（还要为它触发 TOAST 解压），
// 且「列表别带大字段」只能靠每个调用点自律。列白名单写死在 model 里，调用方拿不到多余的列。

// pageArtifactSummaryColumns 列表投影的列白名单（顺序即 SELECT 顺序，测试逐列对账）。
// 刻意不含 source_document / manifest / build_input_manifest。
const pageArtifactSummaryColumns = "id, page_id, version, lang, source_hash, build_input_hash, " +
	"artifact_provider, artifact_key, artifact_hash, compiler_version, registry_version, payload_state, create_time"

// PageArtifactSummary page_artifacts 的列表投影（对应 pageArtifactSummaryColumns）。
//
// 需要源码或清单的路径走详情口径：GetByID / GetByHash / GetByPageVersion —— 它们返回
// 完整实体（含 source_document 与 manifest），服务的是回滚、依赖反序列化与缺文件重建。
type PageArtifactSummary struct {
	ID               string    `gorm:"column:id"`
	PageID           string    `gorm:"column:page_id"`
	Version          int64     `gorm:"column:version"`
	Lang             string    `gorm:"column:lang"`
	SourceHash       string    `gorm:"column:source_hash"`
	BuildInputHash   string    `gorm:"column:build_input_hash"`
	ArtifactProvider string    `gorm:"column:artifact_provider"`
	ArtifactKey      string    `gorm:"column:artifact_key"`
	ArtifactHash     string    `gorm:"column:artifact_hash"`
	CompilerVersion  string    `gorm:"column:compiler_version"`
	RegistryVersion  string    `gorm:"column:registry_version"`
	PayloadState     string    `gorm:"column:payload_state"`
	CreatedAt        time.Time `gorm:"column:create_time"`
}

// gcCandidateColumns GC 候选投影的列白名单。
// 候选只用于「判断能不能删 + 打印清单」，删的依据是 id / hash，从不需要源码与清单。
const gcCandidateColumns = "id, page_id, version, lang, artifact_hash, artifact_key, create_time"

// GCCandidate 可回收候选的列表投影（对应 gcCandidateColumns）。
type GCCandidate struct {
	ID           string    `gorm:"column:id"`
	PageID       string    `gorm:"column:page_id"`
	Version      int64     `gorm:"column:version"`
	Lang         string    `gorm:"column:lang"`
	ArtifactHash string    `gorm:"column:artifact_hash"`
	ArtifactKey  string    `gorm:"column:artifact_key"`
	CreatedAt    time.Time `gorm:"column:create_time"`
}

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

// CreateArtifactTx 在外层事务内插入产物元数据行（page_artifacts）。
//
// 与 CreateArtifactObjectTx / EnsureContentObjectTx 同一批：Record 把「产物行 + 共享内容
// 对象 + 闭包」三类**本模块表**的写入放进同一个事务原子提交（AGENTS.md「model 层定位」
// 允许聚合内原子组合），因此三条写入都以 …Tx 具名方法暴露、句柄由 service 透传 ——
// service 不再在事务回调里拼 Create。唯一冲突是不是业务错误（版本冲突）由 service 判定：
// 那是「映射成哪个业务错误」的决策，不是 SQL，所以本方法只把原始错误原样返回。
//
// SQL 文本与原 service 内联实现逐字一致（GORM 的 Create）。
func (m *Model) CreateArtifactTx(ctx context.Context, tx *gorm.DB, e *PageArtifactEntity) error {
	return tx.WithContext(ctx).Create(e).Error
}

// CreateArtifactObjectTx 在外层事务内插入「产物 → 内容对象」闭包行（page_artifact_objects）。
//
// 调用方必须先写完 content_objects 再写闭包行：content_hash 有外键指向 content_objects，
// 顺序反过来就是「先插引用、后插被引用行」的外键违例（见 ReplaceArtifactContent 同一段注释）。
func (m *Model) CreateArtifactObjectTx(ctx context.Context, tx *gorm.DB, o *PageArtifactObjectEntity) error {
	return tx.WithContext(ctx).Create(o).Error
}

// EnsureContentObjectTx 在外层事务内幂等写入共享内容对象（content_objects，content_hash 为主键）。
//
// 语义：内容寻址下同一 content_hash 代表同一内容字节，其物理位置应当唯一，
// 因此采用 first-writer-wins —— 首个引用该 hash 的 (provider, object_key)
// 即该对象的规范 Locator，后续引用（即使 provider/object_key 不同）只共享
// 对象行，不更新、不覆盖。这是有意的设计权衡而非缺陷：
//   - content_hash 主键约束保证一行一对象，位置唯一，不做多存储冗余登记；
//   - 确定性构建不变量（同输入同产物）保证正常路径下同内容同位置，
//     不同 provider 引用同一 hash 是跨存储冗余信号，首个写入即权威；
//   - 若改为 last-writer-wins，同一对象位置会随引用顺序漂移，
//     破坏内容寻址的不可变语义，且并发写入存在竞态。
//
// 产物行（page_artifacts.artifact_provider/artifact_key）各自记录其自身位置，
// 不受本方法的 first-writer-wins 影响。
//
// 单条 INSERT ... ON CONFLICT DO NOTHING（PG 方言）原子幂等写入，替代原 Count→Create 读-改-写：
// 并发 EnsureRecord 同一 hash 时，原实现双双 Count=0、一方 Create 撞 content_hash 主键，
// 被误报为 ErrArtifactMismatch；ON CONFLICT 在语句内原子消化唯一冲突，无 TOCTOU。
// 冲突 target 必须收窄到 content_hash（主键）：无 target 的 DO NOTHING 会把**任何**唯一冲突
// 都静默吞掉 —— 包括「同一产物的两个文件共用一个 object_key」这种真错，
// 表现为「内容对象行没写进去」，随后闭包插入报外键违例，排查方向直接被带偏。
func (m *Model) EnsureContentObjectTx(ctx context.Context, tx *gorm.DB, contentHash, provider, objectKey string, now time.Time) error {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "content_hash"}},
		DoNothing: true,
	}).Create(&ContentObjectEntity{
		ContentHash: contentHash,
		Provider:    provider,
		ObjectKey:   objectKey,
		ByteSize:    0,
		CreatedAt:   now,
	}).Error
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

// ListStalePageIDs 在**调用方给定的当前产物集合**内挑出 registry_version 与 current
// 不同的产物，返回它们所属的页面 ID（去重、字典序，确定性输出）。
//
// 用途：部署新组件后的全站待重建识别 —— 组件是编译进二进制的，没有运行时事件
// 能提示「已有产物由旧组件产出」，只能靠产物元数据里的版本号比对。
//
// 为什么由调用方给集合，而不是在这里自己扫：判据是「各语言 active/staged 指向的产物」，
// 而那些指针在 page 模块的语言账本（page_publications / page_stagings）里。artifact 只知道
// 「某一行产物是哪个版本产出的」，回答不了「哪一行算当前产物」。旧实现直接扫全部
// payload_state='available' 行，等于把未 GC 的历史回滚产物也当成当前产物：页面重建之后，
// 只要旧产物还在磁盘上，每次重启都会把已经重建过的页面重新标成 stale（报告 ARCH-03）。
//
// 仍保留 payload_state='available' 条件（与旧实现同口径）：已标记回收的行不构成重建理由。
// 入参 id 一律来自账本指针，正常情况下不会命中回收态，这一个条件是幂等兜底。
//
// id = ANY(string_to_array(?, ',')::uuid[]) 而不是 gorm 的 id IN ?：组件升级时入参可能上万
// （全站各语言的 active/staged 产物），IN ? 会展开成同数量的绑定参数、逼近 PostgreSQL 的
// 65535 上限（超限直接报错，结果是「组件更新后一个页面都标不上」）；ANY(数组) 只占一个参数，
// 且仍是单条语句（原子性与 IN ? 相同）。改法与理由与 page_model.go 的 MarkStaleByIDs 一致。
func (m *Model) ListStalePageIDs(ctx context.Context, current string, artifactIDs []string) (ids []string, err error) {
	if len(artifactIDs) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).
		Where("payload_state = ? AND registry_version <> ? AND id = ANY(string_to_array(?, ',')::uuid[])",
			PayloadStateAvailable, current, strings.Join(artifactIDs, ",")).
		Distinct().Order("page_id").Pluck("page_id", &ids).Error
	return ids, err
}

// ListByPage 按版本倒序读取页面的产物记录列表（跨语言，含每个语言的各版本行）。
// 有意不按语言过滤：这是「本页产物全景」视图，语言维度由每行 Lang 自带；
// 需要单语言切片时按 GetByPageVersion 或调用方自行过滤。
//
// 返回**列表投影**（PageArtifactSummary，列白名单 = pageArtifactSummaryColumns）：
// 列表语义不需要 source_document 与 manifest，携带它们只会让每行多付一个可能上 MB 的
// JSONB（审计 DB-02）。需要源码 / 清单时走详情口径的 GetByID / GetByHash / GetByPageVersion。
//
// 当前树内暂无调用方（产物历史视图尚未接线）；保留它作为唯一的列表入口，并在签名上
// 就堵死「顺手 Find 完整实体」这条回头路 —— 谁要列表就用这里返回的投影类型。
func (m *Model) ListByPage(ctx context.Context, pageID string) (list []PageArtifactSummary, err error) {
	err = m.DB(ctx).Select(pageArtifactSummaryColumns).
		Where("page_id = ?", pageID).Order("version DESC, lang ASC").Find(&list).Error
	return list, err
}

// ListGCCandidates 列出可回收候选：
//   - payload_state = available（已标记回收的不重复处理）
//   - create_time 早于 before（保留窗口之外）
//   - 不在 excludeIDs 内（调用方传入的保护集合：页面指针 / 每语言激活暂存 / 路由指向）
//
// excludeIDs 为空表示调用方无法确定保护集合 —— 此时返回空列表（宁可不回收也不误删）。
//
// 返回**列表投影**（GCCandidate，列白名单 = gcCandidateColumns）：GC 每轮按批扫描候选，
// 用完整实体读会把每行的 source_document 与 manifest 一起拽出来，而它们对「这行能不能删」
// 没有任何用（审计 DB-02）。
func (m *Model) ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []GCCandidate, err error) {
	if len(excludeIDs) == 0 {
		return nil, nil
	}
	q := m.DB(ctx).Select(gcCandidateColumns).
		Where("payload_state = ? AND create_time < ?", PayloadStateAvailable, before)
	q = q.Where("id NOT IN ?", excludeIDs)
	err = q.Order("create_time ASC").Find(&list).Error
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
//   - create_time 早于 before（保留窗口之外，避免清理刚落盘、闭包尚未提交的对象）
//   - 不被任何现存产物行引用（见 orphanContentObjectFilter）
//   - hashes 非空时收窄到指定集合（删除时用，避免删掉查完之后才出现的候选）
func (m *Model) orphanContentObjectScope(ctx context.Context, before time.Time, hashes []string) *gorm.DB {
	q := m.db.WithContext(ctx).Table(tableNameContentObjects).
		Where("content_objects.create_time < ?", before).
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

// ListOrphanContentObjects 列出孤儿内容对象（按 create_time 升序，至多 limit 条）。
// limit <= 0 时不加限制 —— 调用方负责给一个有限批次。
func (m *Model) ListOrphanContentObjects(ctx context.Context, before time.Time, limit int) (list []ContentObjectEntity, err error) {
	q := m.orphanContentObjectScope(ctx, before, nil).Select("content_objects.*").Order("content_objects.create_time ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&list).Error
	return list, err
}

// ListStillOrphanHashes 返回给定 hash 中**此刻仍然是孤儿**的子集。
//
// 用途是 GC 删除后的复查：没出现在 DELETE ... RETURNING 里的候选有两种完全不同的成因 ——
//
//	· 被并发归档重新引用（正常赛跑：下一轮它自然不再是候选）；
//	· 删除语句没生效（异常：行还在、且复查时仍无任何引用）。
//
// 只看 RETURNING 的差集分不出这两者，它们都落在差集里；一旦按前者解释，异常就永远静默 ——
// 外键挡删那次就是这样在生产上只增不减的（迁移 204 修的就是它）。
//
// 复用 orphanContentObjectScope：复查与选候选、真删除用的是同一条孤儿判定，
// 三处规则若各写一份，会出现「候选 10、实删 3、复查说还剩 7」这种自相矛盾的报告。
func (m *Model) ListStillOrphanHashes(ctx context.Context, hashes []string, before time.Time) (list []string, err error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	type hashRow struct {
		ContentHash string
	}
	var rows []hashRow
	err = m.orphanContentObjectScope(ctx, before, hashes).
		Select("content_objects.content_hash").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	list = make([]string, 0, len(rows))
	for _, r := range rows {
		list = append(list, r.ContentHash)
	}
	return list, nil
}

// DeleteOrphanContentObjects 删除给定 hash 中**此刻仍是孤儿**的内容对象，
// 返回真正删掉的 hash 列表（RETURNING）。
//
// 复查与删除在同一条语句里完成（而不是先查后删）：查与删之间若有并发归档复用同一
// 内容对象，两者之间的窗口会让「先查后删」删掉刚被引用的行 —— 语句内 NOT EXISTS
// 交给数据库做原子判定。返回集合而不是行数，是为了让调用方能逐条给出「删了 / 被认领了」
// 的准确结论（只报行数时，差值既可能是并发认领也可能是别的意外）。
// 下面这条 Delete 走的是 GORM 的**硬删除**，但这一点依赖一个容易被改掉的细节：
// ContentObjectEntity.DeletedAt 声明为 `*time.Time`，而 GORM v2 只把 `gorm.DeletedAt`
// 类型的字段当软删除标记 —— 类型是裸 `*time.Time` 时它不参与软删除判定，Delete 发出的
// 就是真正的 `DELETE`。若哪天有人把它改成 `gorm.DeletedAt`（看起来更"规范"），
// 这条语句会**静默退化成 UPDATE deleted_at**：GC 之后行还在、「标记清除」白做，
// 而调用方仍然收到一份看起来正常的返回值。
//
// 这个假设由 TestDeleteOrphanContentObjectsReturnsDeletedHashes 兜底（它断言删除后
// 表里剩下的行数），改成软删会立刻变红。
//
// 历史注记：此处原先是手写 `DELETE ... RETURNING` 的 Raw 常量，注释里写着「无论谁怎么改
// 都得是硬删」；改成 GORM 链式后那条注释已不成立，故删除并在实体侧说明依赖。
func (m *Model) DeleteOrphanContentObjects(ctx context.Context, hashes []string, before time.Time) (deleted []string, err error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	// 复用 orphanContentObjectScope（与「看候选」「数候选」同一条判定），
	// RETURNING 走 clause.Returning —— 结果写回传给 Delete 的 slice。
	var gone []ContentObjectEntity
	err = m.orphanContentObjectScope(ctx, before, hashes).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "content_hash"}}}).
		Delete(&gone).Error
	if err != nil {
		return nil, err
	}
	for i := range gone {
		deleted = append(deleted, gone[i].ContentHash)
	}
	return deleted, nil
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
