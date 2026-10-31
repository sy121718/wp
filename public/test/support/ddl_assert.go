package support

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

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

func DDLColumns(t *testing.T, db *gorm.DB, table string) map[string]bool {
	t.Helper()
	var names []string

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

func SortedColumns(cols map[string]bool) string {
	out := make([]string, 0, len(cols))
	for c := range cols {
		out = append(out, c)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

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
