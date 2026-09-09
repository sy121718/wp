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

// contentTranslationQuery 构建期唯一查询形态（§7.7）。
// $1 = text[]（本轮候选 source_hash 集合），$2 = text（目标语言）。
const contentTranslationQuery = `SELECT source_hash, context, target_text
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2`

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
type DBContentStore struct {
	db *gorm.DB
}

// NewDBContentStore 用给定 gorm 句柄构造存储。
func NewDBContentStore(db *gorm.DB) *DBContentStore {
	return &DBContentStore{db: db}
}

// LoadTargets 按 (hashes, lang) 一次批量取回译文。
func (s *DBContentStore) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrContentStoreUnavailable
	}
	return loadContentTargets(ctx, s.db, lang, hashes)
}

// LoadContentTargets 查询层入口：按 (hashes, lang) 一条 SQL 批量查译文，
// 返回 map[ContentIndexKey(hash, context)]target_text。
//
// 不逐条查库：hashes 一次性进 ANY($1)，一次往返取回全部命中。
// hashes 为空 / lang 为空 → 返回空 map（不发查询）。
// 表缺失 / 查询失败 → 返回 error（调用方决定是否回退原文；取词器一律回退）。
func LoadContentTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return loadContentTargets(ctx, db, lang, hashes)
}

// loadContentTargets 实际查询实现（DBContentStore 与 LoadContentTargets 共用）。
func loadContentTargets(ctx context.Context, db *gorm.DB, lang string, hashes []string) (map[string]string, error) {
	targets := make(map[string]string)

	lang = strings.TrimSpace(lang)
	if db == nil {
		return nil, ErrContentStoreUnavailable
	}
	if lang == "" || len(hashes) == 0 {
		return targets, nil
	}

	var rows []contentTranslationRow
	if err := db.WithContext(ctx).Raw(contentTranslationQuery, hashes, lang).Scan(&rows).Error; err != nil {
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

// logContentStoreFailure 记录内容译文不可用（构建继续，原文照常输出）。
func logContentStoreFailure(lang string, err error) {
	fields := map[string]any{"component": "sys_translation"}
	if lang != "" {
		fields["lang"] = lang
	}
	logger.WithFields(fields).Warn("内容译文取数失败，本次构建回退原文: " + err.Error())
}
