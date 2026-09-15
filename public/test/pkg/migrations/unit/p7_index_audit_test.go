package unit

// p7_index_audit_test.go — 审计 P7 索引类四条（IDX-008 / IDX-017 / IDX-018 / IDX-020）的迁移级回归。
//
// 194 只落地两条：删仓守卫的部分索引（IDX-008）与 page_routes 的 route_kind 复合索引（IDX-020）。
// 另外两条逐条核对后判定「无需 DDL」——
//   · IDX-017：三张表在 135 / 120 / 131 已有索引且主查询都走索引，保留期也已由
//     internal/retention/catalog.go 声明；
//   · IDX-018：四个候选比对真实查询面后全部证伪（理由写在 194 的注释里）。
//     删索引需要生产库一个完整业务周期的 idx_scan 增量快照，不是看开发库空表的计数猜出来的。
//
// 所以这条测试除了钉住两个新索引的形态，还钉住「194 里不出现任何 DROP INDEX」——
// 防止后来的人把没核实过的删除塞进这条迁移。

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go_wp/public/migrations"
)

const p7IndexAuditMigrationVersion = "194-p7-index-audit"

// p7AuditIndexNames 本批 194 建出的索引名（判据与断言都以此为准）。
var p7AuditIndexNames = []string{
	"idx_inventory_stocks_warehouse_nonzero",
	"idx_page_routes_project_kind_path",
}

// p7AuditSQL 读取迁移 194 的 SQL 文本。
//
// 刻意不经过 migrations.All()：194 的注册块由父代理统一追加，本文件必须在注册前后
// 都能跑（两条路径取到的 SQL 文本相同）。
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

// stripSQLComments 剥掉逐行注释（-- 到行尾），只留可执行文本。
//
// SplitStatements 会保留注释 —— 它只保证注释里的分号不触发切分，注释本身仍然
// 留在语句里。所以判断语句种类之前必须先剥注释，否则注释里提到的词会污染判定：
// 194 的注释里就写着「本批不执行任何 DROP」。
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

// TestP7IndexAuditMigrationCreatesExpectedIndexes 194 落地后两个索引齐备：
// 删仓守卫是 quantity <> 0 的部分索引，路由索引的键列顺序是 (project_id, route_kind, path)。
//
// 注册之后 migrations.Run 已经跑过 194，这里再逐条执行一遍 —— 正好把
// 「迁移器逐语句执行、不包事务，重跑是唯一恢复手段」所要求的幂等性一并验掉。
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

	// 索引定义与部分索引谓词。
	var rows []pgIdxRow
	if err := db.Raw(`SELECT c.relname AS index_name, pg_get_indexdef(i.indexrelid) AS index_def,
		pg_get_expr(i.indpred, i.indrelid) AS predicate
		FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE c.relname IN ('idx_inventory_stocks_warehouse_nonzero', 'idx_page_routes_project_kind_path')
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

	// 删仓守卫：必须是部分索引，谓词与 CountNonZeroStocks 的 WHERE 一致。
	guard := idx["idx_inventory_stocks_warehouse_nonzero"]
	if guard.Predicate == nil || !strings.Contains(*guard.Predicate, "quantity <> 0") {
		t.Fatalf("idx_inventory_stocks_warehouse_nonzero 应为 quantity <> 0 部分索引，谓词=%v 定义=%s",
			guard.Predicate, guard.IndexDef)
	}
	if !strings.Contains(guard.IndexDef, "warehouse_id") {
		t.Fatalf("删仓守卫索引键列不符: %s", guard.IndexDef)
	}

	// 路由：键列顺序必须是 (project_id, route_kind, path)。
	//
	// 用 pg_index.indkey + pg_attribute 读键列名而不是解析 indexdef —— 与 dependency_fanout
	// 测试同一理由：无函数调用、无跨 schema 访问，也不会与其它测试包的 DROP SCHEMA 竞争。
	var cols []string
	if err := db.Raw(`SELECT a.attname
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE c.relname = 'idx_page_routes_project_kind_path'
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

// TestP7IndexAuditCarriesNoUnverifiedDrop 194 只建索引，不删索引。
//
// IDX-018 的四个候选核对后全部证伪，但「以后要不要删」是会变的 —— 这条断言让变化必须
// 显式发生：谁要往 194 里加 DROP，就得先过这一关，而关卡文案指向的是删索引该有的证据。
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

// TestP7IndexAuditMigrationRegistry 194 的注册判据要按本批自己的索引名枚举计数。
//
// 注册块由父代理追加，未注册时本测试跳过（不阻塞 194 自身的形态断言）；
// 注册之后它会自动生效，并把「用总量 / 用表名」这种会被其它迁移静默满足的写法挡下来。
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
