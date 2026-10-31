package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go_wp/public/migrations"
)

const p7IndexAuditMigrationVersion = "194-p7-index-audit"

var p7AuditIndexNames = []string{
	"idx_inventory_stocks_warehouse_nonzero",
	"idx_page_routes_project_kind_path",
}

func p7AuditSQL(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("运行时无法定位测试文件路径")
	}
	dir := filepath.Dir(thisFile)
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("取工作目录失败: %v", err)
		}
		dir = filepath.Join(wd, dir)
	}
	path := filepath.Clean(filepath.Join(dir, "..", "..", "..", "..", "migrations", "194_p7_index_audit.sql"))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移 194 的 SQL 失败: %v", err)
	}
	return string(b)
}

func stripSQLComments(sql string) string {
	var out []string
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func TestP7IndexAuditMigrationCreatesExpectedIndexes(t *testing.T) {
	db := newMigrationDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行全部迁移失败: %v", err)
	}
	for _, stmt := range migrations.SplitStatements(p7AuditSQL(t)) {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("重跑 194 语句失败（幂等性不成立）: %v\nSQL: %s", err, s)
		}
	}

	var rows []pgIdxRow
	if err := db.Raw(`SELECT c.relname AS index_name, pg_get_indexdef(i.indexrelid) AS index_def,
		pg_get_expr(i.indpred, i.indrelid) AS predicate
		FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		-- 必须限定当前 schema：pg_class 是全库的，并发跑测试时别的隔离 schema 里的同名索引
		-- 会一起被查出来（实测键列被放大成 35 份）。
		WHERE c.relnamespace = current_schema()::regnamespace
		  AND c.relname IN ('idx_inventory_stocks_warehouse_nonzero', 'idx_page_routes_project_kind_path')
		ORDER BY c.relname`).Scan(&rows).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	idx := map[string]pgIdxRow{}
	for _, r := range rows {
		idx[r.IndexName] = r
	}
	if len(idx) != len(p7AuditIndexNames) {
		t.Fatalf("194 应建出 %d 个索引，实际 %d 个: %v", len(p7AuditIndexNames), len(idx), keysOf(idx))
	}

	guard := idx["idx_inventory_stocks_warehouse_nonzero"]
	if guard.Predicate == nil || !strings.Contains(*guard.Predicate, "quantity <> 0") {
		t.Fatalf("idx_inventory_stocks_warehouse_nonzero 应为 quantity <> 0 部分索引，谓词=%v 定义=%s",
			guard.Predicate, guard.IndexDef)
	}
	if !strings.Contains(guard.IndexDef, "warehouse_id") {
		t.Fatalf("删仓守卫索引键列不符: %s", guard.IndexDef)
	}

	var cols []string
	if err := db.Raw(`SELECT a.attname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE c.relnamespace = current_schema()::regnamespace
		  AND c.relname = 'idx_page_routes_project_kind_path'
		ORDER BY k.ord`).Scan(&cols).Error; err != nil {
		t.Fatalf("查询路由索引键列失败: %v", err)
	}
	want := []string{"project_id", "route_kind", "path"}
	if len(cols) != len(want) {
		t.Fatalf("idx_page_routes_project_kind_path 键列数不符: got=%v want=%v", cols, want)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Fatalf("idx_page_routes_project_kind_path 键列顺序不符: got=%v want=%v", cols, want)
		}
	}
}

func TestP7IndexAuditCarriesNoUnverifiedDrop(t *testing.T) {
	stmts := migrations.SplitStatements(p7AuditSQL(t))
	if len(stmts) == 0 {
		t.Fatal("194 的 SQL 未切出任何语句")
	}
	created := 0
	for _, raw := range stmts {
		s := strings.ToUpper(strings.Join(strings.Fields(stripSQLComments(raw)), " "))
		if strings.HasPrefix(s, "DROP ") || strings.Contains(s, "DROP INDEX") {
			t.Fatalf("194 出现未经核实的删除语句: %s\n"+
				"本批结论是四个冗余索引候选全部证伪，删索引需要生产库一个完整业务周期的 idx_scan 增量快照", raw)
		}
		if strings.HasPrefix(s, "CREATE INDEX") {
			created++
			if !strings.Contains(s, "IF NOT EXISTS") {
				t.Fatalf("194 的 CREATE INDEX 必须带 IF NOT EXISTS（迁移器不包事务，重跑是唯一恢复手段）: %s", raw)
			}
		}
	}
	if created != len(p7AuditIndexNames) {
		t.Fatalf("194 应含 %d 条 CREATE INDEX，实际 %d 条", len(p7AuditIndexNames), created)
	}
}

func TestP7IndexAuditMigrationRegistry(t *testing.T) {
	var target *migrations.Migration
	list := migrations.All()
	for i := range list {
		if list[i].Version == p7IndexAuditMigrationVersion {
			target = &list[i]
			break
		}
	}
	if target == nil {
		t.Skipf("迁移 %s 尚未注册到 migrations.All()：需在 register.go 追加注册块后本测试才生效", p7IndexAuditMigrationVersion)
	}
	if !strings.Contains(target.CheckSQL, "?") {
		t.Fatalf("CheckSQL 必须接收迁移器传入的表名参数: %s", target.CheckSQL)
	}
	for _, name := range p7AuditIndexNames {
		if !strings.Contains(target.CheckSQL, name) {
			t.Fatalf("CheckSQL 必须按本批索引名 %s 枚举计数（写总量会被其它迁移的索引满足而静默跳过）: %s",
				name, target.CheckSQL)
		}
	}
}
