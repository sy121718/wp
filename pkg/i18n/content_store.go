package i18n

// content_store.go — sys_translation 的读取端口与唯一查询形态（多语言 P5a，docs/06-D §7.7）。
//
// 构建期取数只有一种形态：一次 ANY($1) 批量查询 + 内存索引；
// 组件渲染期不再查库（§7.7「零查库」）。
//
//	SELECT source_hash, context, target_text
//	  FROM sys_translation
//	 WHERE source_hash = ANY($1) AND lang = $2;
//
// 为什么按 source_hash 过滤而非 context：一个页面的候选 context ≈ 字段数，
// 而 hash 集合一次覆盖所有组件，索引 idx_sys_translation_hash_lang 直接命中；
// 回表后按 context 精确匹配（同一 hash 可能返回多行，不同 context 各自成键）。
//
// 工程作用域（审计 I18N-009）：sys_translation 自迁移 195 起带 project_id，
// NULL = 全局共享（066 既有行的语义），非 NULL = 仅该工程使用。
// 取词规则是**工程行优先、未命中回落全局行**：同一 (source_hash, context) 最多返回一行。
// 调用方没有工程上下文时走「全局作用域」查询（全局行优先）：那时工程行的归属无法
// 判别，取全局行是唯一确定的答案；单站点库里的行全部是全局行，因此结果与 P5a 等价。
//
// 本文件只做读取：写入/工作台（P5c）、组件与 CMS 接入（P5b/P5d）不在本轮范围。

import (
	"context"
	"errors"
	"strings"

	"go_wp/pkg/database"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// ErrContentStoreUnavailable 内容译文存储不可用（数据库未初始化 / 句柄为空）。
//
// 该错误只在本层返回；取词器（NewContentTranslatorWith）会吞掉它并回退原文。
var ErrContentStoreUnavailable = errors.New("内容译文存储不可用")

// contentTranslationQuery 构建期查询形态（§7.7）：**无工程上下文**的全局作用域。
// $1 = text[]（本轮候选 source_hash 集合），$2 = text（目标语言）。
//
// 全局行（project_id IS NULL）优先：无工程上下文时工程行属于谁无从判别，
// 取全局行是唯一确定的答案；单站点库里只有全局行，结果与 P5a 逐字一致。
// DISTINCT ON 同时保证同一 (hash, context) 只回一行 —— 否则同键多行会按物理行序
// 先后覆盖 map，取到哪一条全看 PostgreSQL 的心情。
const contentTranslationQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2
ORDER BY source_hash, context, (project_id IS NULL) DESC, updated_at DESC`

// contentTranslationProjectQuery 工程级查询形态（审计 I18N-009）。
//
// $1 = text[]（候选 source_hash 集合），$2 = text（目标语言），$3 = uuid（当前工程）。
//
// 作用域规则：**工程行优先，未命中回落全局行**（project_id IS NULL）。
// DISTINCT ON 的 ORDER BY 里 (project_id IS NOT NULL) DESC 把工程行排在最前，
// 同一 (source_hash, context) 因此只取一行 —— 一条 SQL 同时完成「工程覆盖 + 全局回落」，
// 不需要在应用层 merge（也就不会出现同键两行互相覆盖的不确定结果）。
// $3 传 NULL（无工程）时 project_id = $3 恒不成立，退化为只读全局行。
const contentTranslationProjectQuery = `SELECT DISTINCT ON (source_hash, context) source_hash, context, target_text
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2 AND (project_id IS NULL OR project_id = $3::uuid)
ORDER BY source_hash, context, (project_id IS NOT NULL) DESC, updated_at DESC`

// contentTranslationRow sys_translation 的读取投影。
type contentTranslationRow struct {
	SourceHash string `gorm:"column:source_hash"`
	Context    string `gorm:"column:context"`
	TargetText string `gorm:"column:target_text"`
}

// ContentStore 是内容译文的读取端口（生产实现 = sys_translation 表）。
//
// 实现方约定：一次调用只允许一条 SQL，返回 map 的键为 ContentIndexKey(hash, context)；
// 表缺失 / 查询失败应返回 error（取词器负责兜底回退原文）。
type ContentStore interface {
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
}

// DBContentStore 基于数据库的 ContentStore 实现。
//
// projectID 为空 = 无工程上下文的全局视图（与 P5a 行为一致）；
// 非空 = 工程作用域：工程行优先、回落全局行（审计 I18N-009）。
type DBContentStore struct {
	db        *gorm.DB
	projectID string
}

// NewDBContentStore 用给定 gorm 句柄构造存储（无工程上下文的全局视图）。
func NewDBContentStore(db *gorm.DB) *DBContentStore {
	return &DBContentStore{db: db}
}

// NewDBContentStoreForProject 构造绑定到某个工程作用域的存储（审计 I18N-009）。
// projectID 为空等价于 NewDBContentStore。
func NewDBContentStoreForProject(db *gorm.DB, projectID string) *DBContentStore {
	return &DBContentStore{db: db, projectID: strings.TrimSpace(projectID)}
}

// ProjectScopedStore 支持派生工程作用域视图的存储实现。
//
// 由 ContentStoreForProject 探测：注入的自定义 store（测试替身 / 缓存层）
// 不实现该接口时原样返回，行为与接入工程作用域之前一致。
type ProjectScopedStore interface {
	ForProject(projectID string) ContentStore
}

// ContentStoreForProject 把任意 ContentStore 绑定到工程作用域。
//
// store 为空返回 nil；store 不实现 ProjectScopedStore 则原样返回（无法按工程隔离时
// 宁可保持既有全局语义，也不猜一个作用域出来）。
func ContentStoreForProject(store ContentStore, projectID string) ContentStore {
	if store == nil {
		return nil
	}
	if scoped, ok := store.(ProjectScopedStore); ok {
		return scoped.ForProject(projectID)
	}
	return store
}

// ForProject 返回绑定到该工程作用域的只读视图（空 projectID 返回自身）。
func (s *DBContentStore) ForProject(projectID string) ContentStore {
	if s == nil {
		return nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return s
	}
	clone := *s
	clone.projectID = projectID
	return &clone
}

// LoadTargets 按 (hashes, lang) 一次批量取回译文（作用域 = 本实例绑定的工程）。
func (s *DBContentStore) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrContentStoreUnavailable
	}
	return loadContentTargets(ctx, s.db, s.projectID, lang, hashes)
}

// LoadContentTargets 查询层入口：按 (hashes, lang) 一条 SQL 批量查译文，
// 返回 map[ContentIndexKey(hash, context)]target_text。
//
// 不逐条查库：hashes 一次性进 ANY($1)，一次往返取回全部命中。
// hashes 为空 / lang 为空 → 返回空 map（不发查询）。
// 表缺失 / 查询失败 → 返回 error（调用方决定是否回退原文；取词器一律回退）。
func LoadContentTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	return LoadContentTargetsForProject(ctx, "", lang, hashes)
}

// LoadContentTargetsForProject 查询层入口：按 (工程, hashes, lang) 批量查译文
// （审计 I18N-009）。projectID 为空退化为 P5a 的全局查询。
func LoadContentTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return loadContentTargets(ctx, db, projectID, lang, hashes)
}

// loadContentTargets 实际查询实现（DBContentStore 与两个入口共用）。
//
// 有工程上下文 → 工程级查询形态；无工程上下文 → 全局查询形态（P5a 原样）。
func loadContentTargets(ctx context.Context, db *gorm.DB, projectID, lang string, hashes []string) (map[string]string, error) {
	targets := make(map[string]string)

	lang = strings.TrimSpace(lang)
	if db == nil {
		return nil, ErrContentStoreUnavailable
	}
	if lang == "" || len(hashes) == 0 {
		return targets, nil
	}

	projectID = strings.TrimSpace(projectID)
	var rows []contentTranslationRow
	query, args := contentTranslationQuery, []any{hashes, lang}
	if projectID != "" {
		query, args = contentTranslationProjectQuery, []any{hashes, lang, projectID}
	}
	if err := db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		// 表上有 CHECK (target_text <> '')，此处再兜一层：空译文等同未命中。
		if row.TargetText == "" {
			continue
		}
		targets[ContentIndexKey(row.SourceHash, row.Context)] = row.TargetText
	}
	return targets, nil
}

// defaultContentStore 返回默认存储（sys_translation 表）。
//
// 每次调用都重新取数据库句柄：数据库未初始化时返回 nil，
// 取词器据此走「空索引 → 全部回退原文」的兜底路径（不报错、不 panic）。
func defaultContentStore() ContentStore {
	db, err := database.GetDB()
	if err != nil {
		logContentStoreFailure("", err)
		return nil
	}
	return NewDBContentStore(db)
}

// defaultProjectContentStore 返回绑定工程作用域的默认存储（审计 I18N-009）。
//
// 与 defaultContentStore 同一兜底口径：数据库未初始化时返回 nil，取词器据此走
// 「空索引 → 全部回退原文」（不报错、不 panic）；projectID 为空即全局视图。
func defaultProjectContentStore(projectID string) ContentStore {
	db, err := database.GetDB()
	if err != nil {
		logContentStoreFailure("", err)
		return nil
	}
	return NewDBContentStoreForProject(db, projectID)
}

// logContentStoreFailure 记录内容译文不可用（构建继续，原文照常输出）。
func logContentStoreFailure(lang string, err error) {
	fields := map[string]any{"component": "sys_translation"}
	if lang != "" {
		fields["lang"] = lang
	}
	logger.WithFields(fields).Warn("内容译文取数失败，本次构建回退原文: " + err.Error())
}
