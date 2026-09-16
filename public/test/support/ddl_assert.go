// ddl_assert.go — 测试基建：按生产 DDL 校验 model 列集合（防 DDL 漂移）。
//
// 由来（PIPE-3 遗留缺陷）：presentation / contenttemplate 的测试此前用
// AutoMigrate 建表，把 model 里多出来的列（如 presentation_instances.status）
// 自动补进测试 schema，于是「model 读写生产库不存在的列」在测试里永远看不见，
// 真实库一写就失败。本文件把判据固化为断言：
//
//	model 的列集合 ⊆ 生产 DDL 的列集合
//
// 配合「测试建表走 migrations.Run（真实迁移 SQL）」使用。
package support

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// ModelColumns 解析 GORM model 的「列名 → 字段名」映射。
func ModelColumns(t *testing.T, dest any) map[string]string {
	t.Helper()
	parsed, err := schema.Parse(dest, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("解析 model %T 失败: %v", dest, err)
	}
	out := make(map[string]string, len(parsed.Fields))
	for _, f := range parsed.Fields {
		out[f.DBName] = f.Name
	}
	return out
}

// DDLColumns 读取当前 schema 下某表的实际列集合。
func DDLColumns(t *testing.T, db *gorm.DB, table string) map[string]bool {
	t.Helper()
	var names []string
	// 走 pg_catalog 而不是 information_schema.columns：后者是多重 join 的视图，无法下推
	// table_schema / table_name 谓词，实测单次约 270ms 且随库里表数增长，而这条断言每个
	// 用例都要跑一次（admin 单元测试 116 个用例就是 31s）。语义一致：information_schema
	// 同样只列出非系统列、非已删除列。
	if err := db.Raw(`SELECT a.attname FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relnamespace = current_schema()::regnamespace
		  AND c.relname = ? AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, table).Scan(&names).Error; err != nil {
		t.Fatalf("读取 %s 列失败: %v", table, err)
	}
	if len(names) == 0 {
		t.Fatalf("表 %s 不存在：测试未按生产迁移建表", table)
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// SortedColumns 便于失败信息阅读。
func SortedColumns(cols map[string]bool) string {
	out := make([]string, 0, len(cols))
	for c := range cols {
		out = append(out, c)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// AssertModelColumnsSubset 断言 model 的列集合 ⊆ 生产 DDL 的列集合。
//
// 多出来的列意味着「model 会读写生产库不存在的列」，真实库上必然失败；
// 少列（model 未映射）不在此断言范围——DDL 的 NOT NULL 列是否落库由
// 业务测试覆盖。
func AssertModelColumnsSubset(t *testing.T, db *gorm.DB, table string, dest any) {
	t.Helper()
	cols := ModelColumns(t, dest)
	ddl := DDLColumns(t, db, table)
	for col, field := range cols {
		if !ddl[col] {
			t.Errorf("model 字段 %s 映射列 %q 在生产 DDL 表 %s 中不存在；生产 DDL 列: %s",
				field, col, table, SortedColumns(ddl))
		}
	}
}
