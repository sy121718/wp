package i18n

// content_write.go — sys_translation 的写入端口与语境解析（多语言 P5c，docs/06-D §7.8）。
//
// P5a 只提供读取路径（content_store.go）；翻译工作台（P5c）是第一个写入口：
// 后台手动填写译文 → 写入 sys_translation（engine = manual）→ 构建期按
// (source_hash, context, lang) 取用。本文件只新增写入与语境解析，
// 不改 P5a 的取词语义与查询层（loadContentTargets / contentTranslationQuery 保持原样）。
//
// 三条硬约束（DDL 挡不住的必须在本层挡）：
//  1. sha256(source_text) 必须等于 source_hash —— 066 迁移的 CHECK 只校验格式，
//     不校验一致性；写错会让「原文已改」的行仍被命中（译文错位到新原文上）。
//  2. target_text 去空白后非空 —— 表上有 CHECK (target_text <> '')，
//     且空译文在构建期等同未命中，写空串是纯粹的噪声行。
//  3. engine 只允许 manual / ai / po（与 066 的 CHECK 一致）。
//
// 幂等：唯一键 (project_id, source_hash, context, lang)（迁移 195 取代 066 的主键，
// NULLS NOT DISTINCT），写入用 ON CONFLICT DO UPDATE，同一条重复保存只更新不新增
//（update_time 推进，ContentRevision 随之变化）。
//
// 工程作用域（审计 I18N-009）：ContentWriteItem.ProjectID 为空 = 全局共享行
//（project_id IS NULL，066 既有行的语义）；非空 = 仅该工程使用。
// 工作台保存的是**本工程**的译文，所以传当前工程 id —— 同一个原文在不同工程
// 各写各的译法，互不覆盖；未写的工程读全局行（读取侧的回落规则见 content_store.go）。

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/database"
	"go_wp/pkg/rls"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 译文来源（engine 列取值，docs/06-D §7.3）：仅用于筛选与审阅，不参与取值逻辑。
const (
	ContentEngineManual = "manual"
	ContentEngineAI     = "ai"
	ContentEnginePO     = "po"
)

// 写入层错误（调用方按 errors.Is 分类；文案可直接展示给编辑者）。
var (
	// ErrContentWriteUnavailable 写入器未绑定数据库句柄 / 数据库未初始化。
	ErrContentWriteUnavailable = errors.New("内容译文存储不可用")
	// ErrContentHashMismatch 原文与 source_hash 不一致（写入前必须重算校验）。
	ErrContentHashMismatch = errors.New("原文与 source_hash 不一致")
	// ErrContentTargetEmpty 译文去空白后为空。
	ErrContentTargetEmpty = errors.New("译文不能为空")
	// ErrContentEngineInvalid engine 不在 manual / ai / po 之内。
	ErrContentEngineInvalid = errors.New("译文来源非法")
	// ErrContentLangEmpty 目标语言为空。
	ErrContentLangEmpty = errors.New("目标语言不能为空")
	// ErrContentContextEmpty 语境为空。
	ErrContentContextEmpty = errors.New("语境不能为空")
	// ErrOrphanProjectEmpty 孤儿检出/清理未指定工程（没有参照系时拒绝执行）。
	ErrOrphanProjectEmpty = errors.New("孤儿清理必须指定工程")
	// ErrContentTxRequired UpsertTx 收到非事务句柄（nil / *sql.DB 连接池）。
	ErrContentTxRequired = errors.New("译文写入需要调用方事务")
	// ErrContentProjectMixed UpsertTx 收到跨工程的一批（调用方须先按工程分组）。
	ErrContentProjectMixed = errors.New("批量写入只能属于同一工程")
	// ErrContentProjectRequired 写入锁没有可用的工程（空白或非合法 uuid，拒绝执行）。
	ErrContentProjectRequired = errors.New("译文写入锁必须指定合法工程")
)

// ParseContentContext 拆解语境 {类型}.{字段名} → (类型, 字段名, ok)。
//
// 字段名不含点号（docs/06-D §7.5 规则 4：字段名只允许字母/数字/下划线），
// 故按最后一个点拆分即可；任一段为空返回 ok=false。
func ParseContentContext(contextName string) (typ, field string, ok bool) {
	ctx := strings.TrimSpace(contextName)
	i := strings.LastIndex(ctx, ".")
	if i <= 0 || i == len(ctx)-1 {
		return "", "", false
	}
	typ, field = strings.TrimSpace(ctx[:i]), strings.TrimSpace(ctx[i+1:])
	if typ == "" || field == "" {
		return "", "", false
	}
	return typ, field, true
}

// ContentWriteItem 一条待写入译文（唯一键 = project_id + source_hash + context + lang）。
type ContentWriteItem struct {
	// ProjectID 工程作用域（审计 I18N-009）：空 = 全局共享行，非空 = 仅该工程使用。
	ProjectID string
	// SourceHash 原文指纹（调用方按 ContentHash(sourceText) 计算；写入时重算校验）。
	SourceHash string
	// Context 语境（组件类型.字段名），如 core.button.text。
	Context string
	// Lang 目标语言，如 en-US。
	Lang string
	// SourceText 原文（冗余存储，供工作台对照与 hash 校验）。
	SourceText string
	// TargetText 译文（去空白后必须非空）。
	TargetText string
	// Engine 译文来源；空 → ContentEngineManual。
	Engine string
}

// ContentTargetInfo 已存在的译文行（工作台徽章与「是否变化」判定用）。
type ContentTargetInfo struct {
	TargetText string
	Engine     string
	UpdatedAt  time.Time
}

// contentTranslationEntity sys_translation 的写入投影。
//
// 不标 primaryKey：迁移 195 起唯一性是 uq_sys_translation_scope_key
// （含可空列 project_id；PG 的主键列不允许 NULL，故它不是主键）。
// 写入走 Upsert 显式声明的 ON CONFLICT 列，不依赖模型层的主键推断。
type contentTranslationEntity struct {
	ProjectID  *string   `gorm:"column:project_id"`
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	Lang       string    `gorm:"column:lang"`
	SourceText string    `gorm:"column:source_text"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

// TableName 绑定 sys_translation（066 迁移）。
func (contentTranslationEntity) TableName() string { return "sys_translation" }

// contentTranslationDetailQuery 工作台读取形态（无工程上下文）：在 (source_hash, lang)
// 索引上多取 engine 与 update_time 两列，作用域规则与构建期一致（全局行优先；
// 同一 (hash, context) 只回一行，理由见 content_store.go 的同名说明）。
const contentTranslationDetailQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text, engine, update_time
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2
ORDER BY source_hash, context, (project_id IS NULL) DESC, update_time DESC`

// contentTranslationDetailProjectQuery 工作台读取形态（工程作用域，审计 I18N-009）：
// 与构建期同一条作用域规则（工程行优先、回落全局行），只是投影多两列。
// $3 = uuid（当前工程）。
const contentTranslationDetailProjectQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text, engine, update_time
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2 AND (project_id IS NULL OR project_id = $3::uuid)
ORDER BY source_hash, context, (project_id IS NOT NULL) DESC, update_time DESC`

// contentTranslationDetailRow 明细查询投影。
type contentTranslationDetailRow struct {
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

// ContentWriter sys_translation 写入器（工作台与未来的 AI 译文共用同一写入路径）。
type ContentWriter struct {
	db *gorm.DB
}

// NewContentWriter 用给定 gorm 句柄构造写入器（测试注入隔离 schema 用）。
func NewContentWriter(db *gorm.DB) *ContentWriter { return &ContentWriter{db: db} }

// NewContentWriterDefault 用默认库（database.GetDB()）构造写入器。
func NewContentWriterDefault() (*ContentWriter, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return &ContentWriter{db: db}, nil
}

// LoadTargets 复用 P5a 读路径按 (hashes, lang) 批量取译文文本（无工程上下文）。
func (w *ContentWriter) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, "", lang, hashes)
}

// LoadTargetsForProject 按 (工程, hashes, lang) 批量取译文文本（工程行优先、回落全局行）。
func (w *ContentWriter) LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error) {
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	return loadContentTargets(ctx, w.db, projectID, lang, hashes)
}

// LoadDetails 按 (hashes, lang) 批量取译文明细（含 engine / update_time）。
//
// 与 LoadTargets 的差别只有投影多两列，查询条件与索引完全相同；
// 供工作台渲染来源徽章、以及「保存是否真的改变了产物」的判定。
func (w *ContentWriter) LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	return w.LoadDetailsForProject(ctx, "", lang, hashes)
}

// LoadDetailsForProject 按 (工程, hashes, lang) 批量取译文明细（审计 I18N-009）。
//
// 作用域规则与构建期一致：工程行优先、未命中回落全局行。工作台据此展示
// 「本工程自己的译法」而不是其它工程的（未覆盖时才显示全局译文）。
func (w *ContentWriter) LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	out := make(map[string]ContentTargetInfo)
	if w == nil || w.db == nil {
		return nil, ErrContentWriteUnavailable
	}
	lang = strings.TrimSpace(lang)
	if lang == "" || len(hashes) == 0 {
		return out, nil
	}
	projectID = strings.TrimSpace(projectID)
	var rows []contentTranslationDetailRow
	if projectID == "" {
		if err := w.db.WithContext(ctx).Raw(
			contentTranslationDetailQuery, hashes, lang).Scan(&rows).Error; err != nil {
			return nil, err
		}
	} else {
		// 工程上下文除 SQL 条件（project_id IS NULL OR project_id = $3）之外**还必须设会话变量**
		// （DB-009）：sys_translation 带 FORCE 策略，未设 app.project_id 时策略只放行
		// project_id IS NULL 的全局行，本工程那几行被静默挡掉 —— 工作台会显示全局译法，
		// 保存前的「是否变化」判定也会把每一行都当成已变更。
		if err := rls.InProjectScope(ctx, w.db, projectID, func(tx *gorm.DB) error {
			return tx.Raw(contentTranslationDetailProjectQuery, hashes, lang, projectID).Scan(&rows).Error
		}); err != nil {
			return nil, err
		}
	}
	for _, row := range rows {
		if row.TargetText == "" {
			continue
		}
		out[ContentIndexKey(row.SourceHash, row.Context)] = ContentTargetInfo{
			TargetText: row.TargetText, Engine: row.Engine, UpdatedAt: row.UpdatedAt,
		}
	}
	return out, nil
}

// contentReadScope 归一之后的读入口入参（两条 Tx 读入口共用同一次 Trim 与空判）。
type contentReadScope struct {
	projectID string
	lang      string
	hashes    []string
}

// empty 报告这次读取是否不发 SQL（空语言 / 空 hashes）。
func (s contentReadScope) empty() bool { return s.lang == "" || len(s.hashes) == 0 }

// normalizeContentReadScope 去除工程与语言的空白（纯函数，不碰数据库）。
func normalizeContentReadScope(projectID, lang string, hashes []string) contentReadScope {
	return contentReadScope{projectID: strings.TrimSpace(projectID), lang: strings.TrimSpace(lang), hashes: hashes}
}

// contentTargetsFromRows 把查询投影收成 map[ContentIndexKey(hash, context)]target_text（空译文等同未命中）。
func contentTargetsFromRows(rows []contentTranslationRow) map[string]string {
	targets := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.TargetText == "" {
			continue
		}
		targets[ContentIndexKey(row.SourceHash, row.Context)] = row.TargetText
	}
	return targets
}

// contentDetailsFromRows 把明细投影收成 map[ContentIndexKey(hash, context)]ContentTargetInfo（空译文等同未命中）。
func contentDetailsFromRows(rows []contentTranslationDetailRow) map[string]ContentTargetInfo {
	out := make(map[string]ContentTargetInfo, len(rows))
	for _, row := range rows {
		if row.TargetText == "" {
			continue
		}
		out[ContentIndexKey(row.SourceHash, row.Context)] = ContentTargetInfo{
			TargetText: row.TargetText, Engine: row.Engine, UpdatedAt: row.UpdatedAt,
		}
	}
	return out
}

// LoadTargetsForProjectTx 在**调用方已开启的事务**内按 (工程, hashes, lang) 批量取译文文本（R1）。
//
// 不自开事务、只在给定 tx 上发一条 SELECT；scope 由调用方保证（同 UpsertTx）。
// 先 requireTx（先判事务），空输入零 SQL；SQL 与旧入口共用常量，保留 DISTINCT ON，无 FOR UPDATE。
func (w *ContentWriter) LoadTargetsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string, hashes []string) (map[string]string, error) {
	// 只判 w == nil：句柄是调用方的 tx，不读 w.db。
	if w == nil {
		return nil, ErrContentWriteUnavailable
	}
	if err := requireTx(tx); err != nil {
		return nil, err
	}
	scope := normalizeContentReadScope(projectID, lang, hashes)
	if scope.empty() {
		return make(map[string]string), nil
	}
	rows, err := loadContentTranslationRowsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	return contentTargetsFromRows(rows), nil
}

// LoadDetailsForProjectTx 在**调用方已开启的事务**内按 (工程, hashes, lang) 批量取译文明细
// （含 engine / update_time；R1）。
//
// 契约同 LoadTargetsForProjectTx：不自开事务、scope 由调用方保证、先 requireTx、
// 空输入零 SQL、无 FOR UPDATE；投影多 engine / update_time 两列。
func (w *ContentWriter) LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string, hashes []string) (map[string]ContentTargetInfo, error) {
	if w == nil {
		return nil, ErrContentWriteUnavailable
	}
	if err := requireTx(tx); err != nil {
		return nil, err
	}
	scope := normalizeContentReadScope(projectID, lang, hashes)
	if scope.empty() {
		return make(map[string]ContentTargetInfo), nil
	}
	var rows []contentTranslationDetailRow
	var err error
	if scope.projectID == "" {
		err = tx.WithContext(ctx).Raw(contentTranslationDetailQuery, scope.hashes, scope.lang).Scan(&rows).Error
	} else {
		err = tx.WithContext(ctx).Raw(
			contentTranslationDetailProjectQuery, scope.hashes, scope.lang, scope.projectID).Scan(&rows).Error
	}
	if err != nil {
		return nil, err
	}
	return contentDetailsFromRows(rows), nil
}

// loadContentTranslationRowsTx 事务内的译文读取（LoadTargetsForProjectTx 用的那一条 SELECT）。
//
// 直接在 tx 上发语句，不套 rls.InProjectScope（它会另开事务）。
func loadContentTranslationRowsTx(ctx context.Context, tx *gorm.DB, scope contentReadScope) ([]contentTranslationRow, error) {
	var rows []contentTranslationRow
	var err error
	if scope.projectID == "" {
		err = tx.WithContext(ctx).Raw(contentTranslationQuery, scope.hashes, scope.lang).Scan(&rows).Error
	} else {
		err = tx.WithContext(ctx).Raw(
			contentTranslationProjectQuery, scope.hashes, scope.lang, scope.projectID).Scan(&rows).Error
	}
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// contentWriteLockSQL 事务级顾问锁（工程级互斥，提交/回滚自动释放）。
//
// 两段都是绑定参数：$1 = 锁键，$2 = 常量 0；键由显式 projectID 派生，不读 current_setting。
const contentWriteLockSQL = "SELECT pg_advisory_xact_lock(hashtextextended($1, $2))"

// contentWriteLockKeyPrefix 锁键前缀（顾问锁是数据库级单一命名空间，靠前缀与其它锁分开）。
const contentWriteLockKeyPrefix = "i18n.content:"

// LockProjectContentWrite 在**调用方已开启的事务**内取得本工程译文写入的事务级顾问锁（R1）。
//
// 不自开事务，锁随调用方事务的提交或回滚释放；同一 tx 可重入；scope 由调用方保证。
// 先 requireTx（先判事务）；工程为空白或非合法 uuid 统一 ErrContentProjectRequired。
// 键用 uuid.Parse 的规范小写形式，大小写不同的同一工程落到同一把锁。
func LockProjectContentWrite(ctx context.Context, tx *gorm.DB, projectID string) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ErrContentProjectRequired
	}
	parsed, err := uuid.Parse(projectID)
	if err != nil {
		return ErrContentProjectRequired
	}
	key := contentWriteLockKeyPrefix + parsed.String() // parsed.String() 即规范小写带连字符形式
	return tx.WithContext(ctx).Exec(contentWriteLockSQL, key, 0).Error
}

// Upsert 批量写入译文
// （ON CONFLICT (project_id, source_hash, context, lang) DO UPDATE）。
//
// 全部条目先校验后写入：任一条不合法 → 整体拒绝（不写半批），
// 避免工作台一次提交里「部分成功」造成难以解释的中间态。
// 作用域由条目自身的 ProjectID 决定（空 = 全局共享行）；不同工程的同 key 译文
// 落在不同的唯一键上，因此各自独立、互不覆盖。
//
// 工程行**必须在会话变量里也设一遍**（DB-009）：sys_translation 带 FORCE 策略，
// 策略的 WITH CHECK 是「本工程行或全局行」—— 不设 app.project_id 时写工程行会被
// 直接拒绝（RLS 的 INSERT 违规是**报错**而不是静默 0 行），工作台表现为「保存译文失败」。
// 会话变量是**单值**的，所以按 ProjectID 分组、在同一事务内逐组 set_config 后再写：
// 整批仍原子（任一组失败整批回滚），每组语句又都在正确的策略谓词下执行。
// 全局行那一组不需要作用域（策略对 project_id IS NULL 恒真）。
// 返回写入条数（含覆盖更新）。
//
// 事务边界与分组**只留在本方法**：UpsertTx 是它的「调用方已开事务」变体，
// 两处入口只共用校验规则与单组执行核心（ON CONFLICT 与校验规则各只有一份），
// 不共用事务与作用域：分组与逐组 scope 在这里（upsertContentGroupsTx），
// 核心 upsertContentTranslationsTx 只负责把一组行写下去。
func (w *ContentWriter) Upsert(ctx context.Context, items []ContentWriteItem) (written int, err error) {
	if w == nil || w.db == nil {
		return 0, ErrContentWriteUnavailable
	}
	if len(items) == 0 {
		return 0, nil
	}
	err = w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var werr error
		written, werr = w.upsertContentGroupsTx(ctx, tx, items)
		return werr
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// UpsertTx 在**调用方已开启的事务**内批量写入译文（导航译文改造 T1）。
//
// 与 Upsert 的精确分工（两处写入口只共用校验规则与 ON CONFLICT 写核心，不共用事务与作用域）：
//   - 事务：本方法**不自开事务**、不 commit / rollback，句柄是调用方的 tx。
//     失败只把错误（原样，不包装、不吞）交回去，由外层 Transaction 回滚 ——
//     同一事务里前后其它写入随之回滚，不会留下「译文已写、会话没写」的半截状态。
//     Upsert 相反：它对**每个**工程段自行开一个事务并逐段 set_config。
//   - 作用域（本方法的前置条件，不是它做的事）：本方法**不读、不设、不改**
//     调用方的 app.project_id。写工程行之前，调用方必须在**同一个 tx** 上先
//     rls.ScopeTx(tx, projectID)（或整个动作包在 rls.InProjectScope 里）：
//     sys_translation 带 FORCE 策略（迁移 215），WITH CHECK 是「本工程行或全局行」，
//     没设变量时写工程行会被数据库**直接报错**（RLS 的 INSERT 违规是错误而不是 0 行）。
//     全局行（ProjectID 为空）不需要作用域，策略谓词对它恒真。
//   - 工程段：单次只接受**同一工程**的一批（ProjectID 全空 = 全局行，也算一组）。
//     混工程在**任何写入之前**整体拒绝（ErrContentProjectMixed）——
//     会话变量是单值的，本方法不允许自己回头改调用方已经设好的作用域。
//     需要一次写多个工程，用 Upsert（它按组分组、逐组 scope）。
//   - 计数：返回到校验通过并写入的条数（含 ON CONFLICT 覆盖更新），口径与 Upsert 相同。
//
// 校验沿用 normalizeContentWriteItem（与 Upsert 同一份规则）：任一条不合法 → 零写入
// （校验全部在内存里做完，此时没有任何 SQL 发出）。
// 事务判别见 requireTx：nil / typed-nil / 连接池句柄 / 已失效句柄 → ErrContentTxRequired，
// 零写入、不 panic。空批与事务判别的先后：**先判事务**，故空批 + 非事务句柄同样被拒绝
// （与 Upsert 的空批短路相反：Upsert 不需要事务，UpsertTx 需要）。
func (w *ContentWriter) UpsertTx(ctx context.Context, tx *gorm.DB, items []ContentWriteItem) (written int, err error) {
	// 只判构造器约定（w == nil）：本方法只依赖外部 tx，不读 w.db；
	// ContentWriter 的既有构造规则是「NewContentWriter(db) 收下任意句柄，使用点各自判 w.db」，
	// 这里不为表面对称去加 w.db == nil。
	if w == nil {
		return 0, ErrContentWriteUnavailable
	}
	if err := requireTx(tx); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	group, err := normalizeContentWriteGroup(items, now)
	if err != nil {
		return 0, err
	}
	if werr := upsertContentTranslationsTx(tx, group.rows); werr != nil {
		return 0, werr
	}
	return len(group.rows), nil
}

// upsertContentGroupsTx 校验 → 按工程分组 → 逐组写（不自开事务，句柄由调用方给）。
//
// 只在 Upsert 的事务包装里使用：会话变量是单值的，多组必须逐组 set_config 后再写。
func (w *ContentWriter) upsertContentGroupsTx(_ context.Context, tx *gorm.DB, items []ContentWriteItem) (written int, err error) {
	now := time.Now().UTC()
	rows := make([]contentTranslationEntity, 0, len(items))
	for i := range items {
		row, verr := normalizeContentWriteItem(items[i], now)
		if verr != nil {
			return 0, verr
		}
		rows = append(rows, row)
	}
	groups := make([]contentWriteGroup, 0, 2)
	groupIndex := make(map[string]int, 2)
	for i := range rows {
		key := ""
		if rows[i].ProjectID != nil {
			key = *rows[i].ProjectID
		}
		gi, ok := groupIndex[key]
		if !ok {
			groups = append(groups, contentWriteGroup{projectID: key})
			gi = len(groups) - 1
			groupIndex[key] = gi
		}
		groups[gi].rows = append(groups[gi].rows, rows[i])
	}
	for gi := range groups {
		g := &groups[gi]
		if g.projectID != "" {
			if serr := rls.ScopeTx(tx, g.projectID); serr != nil {
				return 0, serr
			}
		}
		if werr := upsertContentTranslationsTx(tx, g.rows); werr != nil {
			return 0, werr
		}
	}
	return len(rows), nil
}

// normalizeContentWriteGroup 校验一批并归到同一工程组里；混工程 → ErrContentProjectMixed。
//
// 与 Upsert 的差别只有一处判据：Upsert 允许一批里出现多个工程（它自己能逐组 scope），
// UpsertTx 不能（不能打断调用方的 scope）。校验仍然先于任何写入 —— 混工程在第一条
// 行归一化时就能判定，此时还没有任何 SQL 发出。
func normalizeContentWriteGroup(items []ContentWriteItem, now time.Time) (contentWriteGroup, error) {
	rows := make([]contentTranslationEntity, 0, len(items))
	groupID := ""
	for i := range items {
		row, err := normalizeContentWriteItem(items[i], now)
		if err != nil {
			return contentWriteGroup{}, err
		}
		id := ""
		if row.ProjectID != nil {
			id = *row.ProjectID
		}
		if i == 0 {
			groupID = id
		} else if id != groupID {
			return contentWriteGroup{}, ErrContentProjectMixed
		}
		rows = append(rows, row)
	}
	return contentWriteGroup{projectID: groupID, rows: rows}, nil
}

// requireTx 判定句柄是不是调用方已开启的、可用的真事务（不新开、不改它）。
//
// 判据顺序（与实现逐行一致）：先判 tx / Statement / ConnPool 三个 nil（任一命中 →
// ErrContentTxRequired），再原样交回 tx.Error（见下），最后按具体类型判真事务句柄。
//
// 判据：Statement.ConnPool 必须是真事务句柄。本机 gorm.io/gorm@v1.31.2 里只有两种：
//   - *sql.Tx —— 普通事务（finisher_api.go:699-703 把 ConnPool 换成 BeginTx 的返回值）；
//   - *gorm.PreparedStmtTX —— Config.PrepareStmt=true 时对上面那个事务的包装
//     （gorm.go:313-320：原始 ConnPool 本身是事务（gorm.Tx）时才包成 PreparedStmtTX，
//     否则包成 PreparedStmtDB）。
//
// 连接池句柄是 *sql.DB，两种都不是。
//
// 为什么不「非 nil 即可」：连接池上各语句各自提交，「回滚」根本不存在，
// 调用方以为的原子性会被静默丢掉。
//
// 为什么不照抄「对任意值 reflect.IsNil 判空」：reflect.Value.IsNil 只对
// chan/func/interface/map/pointer/slice 合法，对 struct 等其它 Kind 直接 panic。
// 这里要挡的只有「接口本身是 nil」与「接口里装 typed-nil」两种：
//   - 接口本身 nil → 上面的 `== nil` 已挡（未设类型的事务句柄，ConnPool 就是 nil）；
//   - typed-nil：&gorm.PreparedStmtTX{} 或 Tx 字段为 (*sql.Tx)(nil)。`== nil` 对它为假，
//     而 PreparedStmtTX.ExecContext 会把 tx.Tx 直接交给 prepare（prepare_stmt.go:170-178，
//     无判空）→ panic。所以取出来按具体类型判字段即可，不引入反射。
//
// 不把 gorm.TxCommitter 当判据的原因：它只有 Commit/Rollback 两个方法，
// 任何自造的伪事务（两行空实现）都能满足；而本仓唯一的驱动是
// gorm.io/driver/postgres v1.6.3，ConnPool 赋值处只有 postgres.go:92（*sql.DB，经 stdlib.OpenDB）
// 与 finisher_api.go:699-703（BeginTx 的返回值，即 *sql.Tx）。
// 不认识的 ConnPool 一律 ErrContentTxRequired（fail closed，不会静默降级成自动提交）。
//
// tx.Error：它记录的是这个会话上下文上已经发生的错误（最常见是 Begin 失败，
// 见 finisher_api.go:706-711 的 tx.AddError，也包含多语句 SQL 的提前失败）。
// 这些错误**原样**交回调用方（不吞、不改写成事务判定错误，便于归属）；
// 只有「无错误」才继续判句柄。
//
// 谁先谁后：tx.Error 的检查在三个 nil 之后、类型判定之前。所以
//   - tx / Statement / ConnPool 任一为 nil 时，即使 tx.Error 非 nil 也返回
//     ErrContentTxRequired（句柄形状不成立 —— fail closed 优先）；
//   - 三者都在时，tx.Error 优先于类型判定：一个 ConnPool 形状不认识、但携带真实
//     失败原因的句柄，拿到的是那个真实原因而不是 ErrContentTxRequired
//     （TestUpsertTxReturnsTxError 覆盖的正是这个形状）。
func requireTx(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil || tx.Statement.ConnPool == nil {
		return ErrContentTxRequired
	}
	if err := tx.Error; err != nil {
		return err
	}
	switch conn := tx.Statement.ConnPool.(type) {
	case *sql.Tx:
		if conn == nil {
			return ErrContentTxRequired
		}
		return nil
	case *gorm.PreparedStmtTX:
		// Tx 是内嵌的 gorm.Tx 接口：nil 接口或 (*sql.Tx)(nil) 的 typed-nil 都会在写路径上 panic。
		if conn == nil || conn.Tx == nil {
			return ErrContentTxRequired
		}
		return nil
	default:
		return ErrContentTxRequired
	}
}

// contentWriteGroup 同一工程作用域下的一批写入行（全局行归入 projectID == "" 组）。
type contentWriteGroup struct {
	projectID string
	rows      []contentTranslationEntity
}

// upsertContentTranslationsTx 一批译文行的 ON CONFLICT DO UPDATE 写入（单组执行核心）。
//
// 独立成函数的原因：Upsert（事务包装 + 分组）与 UpsertTx（直接用调用方事务）都要写，
// 而 ON CONFLICT 的冲突列声明只能有一处 —— 复制第二份必然漂移。
// 本函数不自开事务、不设作用域：句柄与 scope 由调用方准备好。
func upsertContentTranslationsTx(tx *gorm.DB, rows []contentTranslationEntity) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "source_hash"}, {Name: "context"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_text", "target_text", "engine", "update_time"}),
	}).Create(&rows).Error
}

// normalizeContentWriteItem 校验并归一单条写入（返回可直接入库的行）。
func normalizeContentWriteItem(item ContentWriteItem, now time.Time) (row contentTranslationEntity, err error) {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" {
		return row, ErrContentLangEmpty
	}
	contextName := strings.TrimSpace(item.Context)
	if contextName == "" {
		return row, ErrContentContextEmpty
	}
	target := strings.TrimSpace(item.TargetText)
	if target == "" {
		return row, ErrContentTargetEmpty
	}
	// 原文指纹一致性：066 迁移只校验 hash 格式，一致性必须在此处挡。
	if item.SourceHash != ContentHash(item.SourceText) {
		return row, ErrContentHashMismatch
	}
	engine := strings.TrimSpace(item.Engine)
	if engine == "" {
		engine = ContentEngineManual
	}
	switch engine {
	case ContentEngineManual, ContentEngineAI, ContentEnginePO:
	default:
		return row, ErrContentEngineInvalid
	}
	// 工程作用域：空 = 全局共享行（project_id IS NULL），非空 = 仅该工程使用。
	var projectID *string
	if scoped := strings.TrimSpace(item.ProjectID); scoped != "" {
		projectID = &scoped
	}
	return contentTranslationEntity{
		ProjectID: projectID, SourceHash: item.SourceHash, Context: contextName, Lang: lang,
		SourceText: item.SourceText, TargetText: target, Engine: engine, UpdatedAt: now,
	}, nil
}
