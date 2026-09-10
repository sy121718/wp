package pluginservice

// plugin_migrate.go — 插件 L1 数据层迁移执行器（docs/06-plugin-system.md §8）。
//
// 每个插件一个独立 PG schema（plugin_<id>），表名零冲突；迁移 SQL 由插件
// zip 的 {Migrations}/ 目录携带（版本化 .sql 文件，按文件名字典序执行），
// 文件内自行包含 CREATE SCHEMA IF NOT EXISTS plugin_<id> + 建表语句——
// 执行器不代写 schema 语句，只负责逐条执行（复用 public/migrations 的
// SplitStatements 拆分多语句，尊重 DO $$ 块 / -- 注释 / 字符串字面量内的分号）。

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go_wp/internal/builder/plugincomp"
	pluginmodel "go_wp/internal/module/plugin/model"
	"go_wp/public/migrations"

	"gorm.io/gorm"
)

// schemaPrefix L1 数据层 schema 名前缀（docs/06 §8）。
const schemaPrefix = "plugin_"

// schemaNameFor 由插件 ID 派生 schema 名（plugin_<id>）。
func schemaNameFor(pluginID string) string {
	return schemaPrefix + pluginID
}

// dropSchemaSQL 生成 DROP SCHEMA 语句（标识符加双引号，兼容含连字符的 ID；
// 插件 ID 经 plugincomp 白名单校验，不含双引号/反斜杠，拼接无注入面）。
func dropSchemaSQL(pluginID string) string {
	return `DROP SCHEMA IF EXISTS "` + schemaNameFor(pluginID) + `" CASCADE`
}

// migrateSchema 执行插件 L1 数据层迁移，返回 nil 表示成功或无需迁移。
//
// 版本策略（开发阶段从简，AGENTS.md 允许不兼容旧数据、直接重构）：
//   - 空 Migrations（纯展示插件）→ 不执行任何迁移，直接返回 nil；
//   - 全新安装（existing == nil）→ 按文件名字典序执行全部迁移文件；
//   - 升级（existing.SchemaVersion != m.SchemaVersion）→ 采用「重装 =
//     DROP SCHEMA CASCADE 后跑全量」的简单策略，不做「文件名序号 > 旧版本」
//     的增量比对——迁移文件内自带 CREATE SCHEMA IF NOT EXISTS，重跑全量天然幂等；
//   - 同版本（existing.SchemaVersion == m.SchemaVersion）→ 幂等跳过，只更新元数据。
//
// 整体包在一个事务内执行，任一条语句失败即整体回滚，不留半成品 schema。
func (s *Service) migrateSchema(ctx context.Context, m *plugincomp.Manifest, files map[string][]byte, existing *pluginmodel.Entity) (err error) {
	if m.Migrations == "" {
		return nil // 纯展示插件，无自有表
	}
	if existing != nil && existing.SchemaVersion == m.SchemaVersion {
		return nil // 同版本重复安装，幂等跳过
	}
	// 收集 {Migrations}/ 目录下 *.sql 文件，按文件名字典序排序（版本化顺序）。
	names := collectMigrationSQL(m.Migrations, files)
	if len(names) == 0 {
		return fmt.Errorf("迁移目录 %q 下无 .sql 文件", m.Migrations)
	}
	schemaName := schemaNameFor(m.ID)
	// 单事务执行：重装先 DROP 旧 schema，再逐条执行全部迁移。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if existing != nil {
			if err := tx.Exec(dropSchemaSQL(m.ID)).Error; err != nil {
				return fmt.Errorf("清理旧 schema %s: %w", schemaName, err)
			}
		}
		// 锁定 search_path：迁移里未限定 schema 的对象全部落在插件自己的 schema，
		// 不会外溢到 public（与 validatePluginStatement 的 public. 拒绝互为纵深）。
		if err := tx.Exec("SET LOCAL search_path TO " + quoteSchemaIdent(schemaName)).Error; err != nil {
			return fmt.Errorf("锁定 search_path 到 %s 失败: %w", schemaName, err)
		}
		for _, name := range names {
			for _, stmt := range migrations.SplitStatements(string(files[name])) {
				if stmt == "" {
					continue
				}
				if verr := validatePluginStatement(stmt); verr != nil {
					return fmt.Errorf("迁移文件 %s: %w", name, verr)
				}
				if err := tx.Exec(stmt).Error; err != nil {
					return fmt.Errorf("迁移文件 %s 执行失败: %w", name, err)
				}
			}
		}
		return nil
	})
}

// pluginSQLDeny 插件迁移语句的危险模式黑名单。
//
// 背景（全项目审查发现）：迁移 SQL 直接来自插件 zip，此前逐条 tx.Exec 无任何
// 限制 —— 拥有 plugin:install 权限的低权管理员可借插件包执行任意 SQL（等价提权 DBA，
// 可 INSERT sys_admin / 改 sys_casbin_rule）。插件迁移的正当需求只有「在自己的
// plugin_<id> schema 下建表、建索引、填默认数据」，因此执行器加了两道防线：
//  1. 执行前 SET LOCAL search_path 锁定到插件自己的 schema（未限定对象不外溢）；
//  2. 下列模式一律拒绝（跨库破坏、角色与权限变更、文件与外部访问、系统目录直读）。
var pluginSQLDeny = []*regexp.Regexp{
	regexp.MustCompile(`\bdrop\s+(database|schema|tablespace)\b`),
	regexp.MustCompile(`\b(create|alter|drop)\s+(role|user)\b`),
	regexp.MustCompile(`\bgrant\b|\brevoke\b`),
	regexp.MustCompile(`\balter\s+system\b`),
	regexp.MustCompile(`\bset\s+role\b`),
	regexp.MustCompile(`\bsecurity\s+definer\b`),
	regexp.MustCompile(`\bcopy\b`),
	regexp.MustCompile(`\bpg_read_file\b|\bpg_write_file\b|\bpg_ls_dir\b|\bpg_read_binary_file\b`),
	regexp.MustCompile(`\blo_import\b|\blo_export\b`),
	regexp.MustCompile(`\bpg_authid\b|\bpg_shadow\b|\bpg_catalog\b|\binformation_schema\b`),
	regexp.MustCompile(`\bpublic\s*\.`),
	regexp.MustCompile(`\bdblink\b|\bpostgres_fdw\b`),
}

// sqlBlockCommentRe 块注释（校验前剥离，避免注释文本触发误判）。
var sqlBlockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

// stripSQLComments 剥离行注释与块注释，只对真正的语句文本做关键字匹配。
func stripSQLComments(stmt string) string {
	s := sqlBlockCommentRe.ReplaceAllString(stmt, " ")
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// validatePluginStatement 校验单条插件迁移语句，命中黑名单即拒绝执行。
func validatePluginStatement(stmt string) error {
	lower := strings.ToLower(stripSQLComments(stmt))
	for _, re := range pluginSQLDeny {
		if re.MatchString(lower) {
			return fmt.Errorf("迁移语句被安全策略拒绝（命中 %s）：插件迁移只能操作自己的 schema", re.String())
		}
	}
	return nil
}

// quoteSchemaIdent 双引号包裹 schema 标识符（ID 经 plugincomp 白名单校验，无注入面）。
func quoteSchemaIdent(name string) string {
	return `"` + name + `"`
}

// collectMigrationSQL 收集迁移目录下 *.sql 文件的键，按字典序排序返回。
func collectMigrationSQL(dir string, files map[string][]byte) []string {
	prefix := dir + "/"
	var names []string
	for key := range files {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ".sql") {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names
}
