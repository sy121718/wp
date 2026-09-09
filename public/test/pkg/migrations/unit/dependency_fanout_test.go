// 071（依赖 fan-out）的迁移级回归（PIPE-3）。
//
// 两张依赖表由 002-init-builder-schema 创建，071 必须靠「索引名 + 约束定义」判定
// 是否执行（默认的「表存在即跳过」必然误跳过，索引与约束扩展永远落不下来）；
// 同时保证反查索引的列顺序是 (dependency_kind, dependency_key)——与主键前导列
// artifact_id 相反，正是 fan-out 查询的方向。
package unit

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
)

const depFanoutMigrationVersion = "071-dependency-fanout"

// TestDependencyFanoutMigrationBuildsIndexesAndExtendsKinds 全量迁移后：
// 反查索引齐备、列顺序正确，且两表 CHECK 约束都接受 'i18n' / 'block'。
func TestDependencyFanoutMigrationBuildsIndexesAndExtendsKinds(t *testing.T) {
	db := newMigrationDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行全部迁移失败: %v", err)
	}

	// 1) 反查索引存在且列顺序为 (dependency_kind, dependency_key)。
	//
	// 刻意不用 pg_get_indexdef / pg_indexes.indexdef：它们会对全库索引求值，
	// 与其它测试包并发 DROP SCHEMA 竞争（could not open relation with OID）。
	// 用 pg_index.indkey + pg_attribute 直接读键列名，无函数调用、无跨 schema 访问。
	var idxRows []struct {
		IndexName string
		Columns   string
	}
	if err := db.Raw(`SELECT c.relname AS index_name,
			string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum)) AS columns
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid IN ('page_dependencies'::regclass, 'presentation_dependencies'::regclass)
		  AND c.relname IN ('idx_page_deps_lookup', 'idx_pres_deps_lookup')
		GROUP BY c.relname ORDER BY c.relname`).Scan(&idxRows).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if len(idxRows) != 2 {
		t.Fatalf("期望 2 个反查索引，实际 %d: %+v", len(idxRows), idxRows)
	}
	for _, row := range idxRows {
		if row.Columns != "dependency_kind,dependency_key" {
			t.Fatalf("索引 %s 列顺序应为 (dependency_kind, dependency_key)，实际 %q", row.IndexName, row.Columns)
		}
	}

	// 2) CHECK 约束接受新增的 i18n / block 两种 kind。
	var defs []string
	// 按 conrelid 限定到当前 search_path 下的两张表：pg_constraint 是全局视图，
	// 只按 conname 过滤会把 public（真实库）里的同名约束一起捞出来；
	// MATERIALIZED CTE 保证 pg_get_constraintdef 只对本表约束求值（见 register.go 071）。
	if err := db.Raw(`WITH target AS MATERIALIZED (
			SELECT oid FROM pg_constraint
			WHERE conrelid IN ('page_dependencies'::regclass, 'presentation_dependencies'::regclass)
			  AND conname IN ('page_dependencies_dependency_kind_check', 'presentation_dependencies_dependency_kind_check'))
		SELECT pg_get_constraintdef(t.oid) FROM target t ORDER BY 1`).Scan(&defs).Error; err != nil {
		t.Fatalf("查询约束失败: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("期望 2 个 dependency_kind CHECK 约束，实际 %d", len(defs))
	}
	for _, def := range defs {
		for _, kind := range []string{"i18n", "block", "direct_content", "content_collection"} {
			if !strings.Contains(def, "'"+kind+"'") {
				t.Fatalf("CHECK 约束缺少 kind %q: %s", kind, def)
			}
		}
	}

	// 3) 迁移可重复执行（幂等）：第二次全部跳过且不报错。
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移二次执行失败（应幂等）: %v", err)
	}
}

// TestDependencyFanoutMigrationCheckSQL 表已存在但索引/约束缺失时必须判定为「未完成」。
func TestDependencyFanoutMigrationCheckSQL(t *testing.T) {
	var target *migrations.Migration
	for i := range migrations.All() {
		if migrations.All()[i].Version == depFanoutMigrationVersion {
			m := migrations.All()[i]
			target = &m
			break
		}
	}
	if target == nil {
		t.Fatalf("迁移 %s 未注册到 migrations.All()", depFanoutMigrationVersion)
	}
	if !strings.Contains(target.CheckSQL, "idx_page_deps_lookup") || !strings.Contains(target.CheckSQL, "i18n") {
		t.Fatalf("071 的 CheckSQL 必须同时按索引名与约束定义判定，实际: %s", target.CheckSQL)
	}

	db := newMigrationDB(t)
	// 只建「旧形状」依赖表（002 的原始定义，无新索引、CHECK 无 i18n）。
	if err := db.Exec(`CREATE TABLE page_dependencies (
		page_id uuid NOT NULL, artifact_id uuid NOT NULL, dependency_kind text NOT NULL,
		dependency_key text NOT NULL, revision text NULL, last_checked timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (artifact_id, dependency_kind, dependency_key),
		CHECK (dependency_kind IN ('direct_content', 'content_collection', 'content_template',
			'menu', 'media', 'global_component', 'site_setting', 'runtime')))`).Error; err != nil {
		t.Fatalf("建 page_dependencies 失败: %v", err)
	}
	if err := db.Exec(`CREATE TABLE presentation_dependencies (
		presentation_id uuid NOT NULL, artifact_id uuid NOT NULL, dependency_kind text NOT NULL,
		dependency_key text NOT NULL, revision text NULL, last_checked timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (artifact_id, dependency_kind, dependency_key),
		CHECK (dependency_kind IN ('direct_content', 'content_collection', 'content_template',
			'menu', 'media', 'global_component', 'site_setting', 'runtime')))`).Error; err != nil {
		t.Fatalf("建 presentation_dependencies 失败: %v", err)
	}
	var count int64
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&count).Error; err != nil {
		t.Fatalf("CheckSQL 执行失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("表存在但索引/约束未扩展时 CheckSQL 应为 0（触发执行），实际 %d", count)
	}

	// 补上索引与扩展约束后，CheckSQL 应返回 >0（跳过）。
	if err := db.Exec("CREATE INDEX idx_page_deps_lookup ON page_dependencies (dependency_kind, dependency_key)").Error; err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	if err := db.Exec("CREATE INDEX idx_pres_deps_lookup ON presentation_dependencies (dependency_kind, dependency_key)").Error; err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	for _, stmt := range []string{
		"ALTER TABLE page_dependencies DROP CONSTRAINT page_dependencies_dependency_kind_check",
		"ALTER TABLE page_dependencies ADD CONSTRAINT page_dependencies_dependency_kind_check CHECK (dependency_kind IN ('direct_content', 'i18n', 'block'))",
		"ALTER TABLE presentation_dependencies DROP CONSTRAINT presentation_dependencies_dependency_kind_check",
		"ALTER TABLE presentation_dependencies ADD CONSTRAINT presentation_dependencies_dependency_kind_check CHECK (dependency_kind IN ('direct_content', 'i18n', 'block'))",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行 %q 失败: %v", stmt, err)
		}
	}
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&count).Error; err != nil {
		t.Fatalf("CheckSQL 二次执行失败: %v", err)
	}
	if count == 0 {
		t.Fatalf("索引与约束齐备时 CheckSQL 应 >0（跳过），实际 %d", count)
	}
}
