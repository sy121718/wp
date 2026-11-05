package permission_test

import (
	"context"
	"testing"

	"go_wp/internal/permission"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func countRows(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("查询失败 %q: %v", query, err)
	}
	return n
}

func strValue(t *testing.T, db *gorm.DB, query string, args ...any) string {
	t.Helper()
	var s string
	if err := db.Raw(query, args...).Scan(&s).Error; err != nil {
		t.Fatalf("查询失败 %q: %v", query, err)
	}
	return s
}

func TestSyncToDBIsIdempotentAndKeepsManualGrants(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()

	if err := db.Exec(`INSERT INTO sys_admin (id, username, password, status, is_admin)
		VALUES (1, 'sync_admin', 'x', 1, 1) ON CONFLICT (id) DO NOTHING`).Error; err != nil {
		t.Fatalf("准备超管账号失败: %v", err)
	}

	if err := db.Exec("DELETE FROM sys_casbin_rule").Error; err != nil {
		t.Fatalf("清空策略表失败: %v", err)
	}
	if err := db.Exec("DELETE FROM sys_permission").Error; err != nil {
		t.Fatalf("清空权限点表失败: %v", err)
	}

	permission.Reset()
	permission.Declare("GET", "/api/test/widget/list", permission.ProductList)
	permission.Declare("POST", "/api/test/widget/create", permission.ProductCreate)

	first, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("首次同步失败: %v", err)
	}
	if first.Inserted != 2 {
		t.Fatalf("首次同步应新增 2 条权限点，实际 %d", first.Inserted)
	}
	if first.SuperPolicies != 2 {
		t.Fatalf("首次同步应为超管补 2 条策略，实际 %d", first.SuperPolicies)
	}
	if got := strValue(t, db,
		`SELECT api_path FROM sys_permission WHERE permission_code = 'product:list'`); got != "/api/test/widget/list" {
		t.Fatalf("权限点路径写错: %q", got)
	}

	second, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("第二次同步失败: %v", err)
	}
	if second.Inserted != 0 || second.PathFixed != 0 || second.PolicyFixed != 0 || second.SuperPolicies != 0 {
		t.Fatalf("第二次同步应完全幂等，实际 %s", second.String())
	}

	if err := db.Exec(`UPDATE sys_permission SET permission_name = '人工改名', status = 0
		WHERE permission_code = 'product:list'`).Error; err != nil {
		t.Fatalf("模拟人工改名 / 禁用失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', 'editor', '/api/test/widget/list', 'GET', 'product:list')`).Error; err != nil {
		t.Fatalf("模拟人工角色授权失败: %v", err)
	}
	if err := db.Exec(`DELETE FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = '1' AND v3 = 'product:create'`).Error; err != nil {
		t.Fatalf("模拟人工删除超管策略失败: %v", err)
	}

	third, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("第三次同步失败: %v", err)
	}

	if got := strValue(t, db,
		`SELECT permission_name FROM sys_permission WHERE permission_code = 'product:list'`); got != "人工改名" {
		t.Fatalf("人工改过的权限点名称被覆盖: %q", got)
	}

	if got := countRows(t, db,
		`SELECT count(*) FROM sys_permission WHERE permission_code = 'product:list' AND status = 1`); got != 0 {
		t.Fatalf("被人工禁用的权限点被自动复用了")
	}

	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = 'editor'`); got != 1 {
		t.Fatalf("人工角色授权被删除，剩余 %d 行", got)
	}

	if third.SuperPolicies != 1 {
		t.Fatalf("应补回 1 条超管策略，实际 %d", third.SuperPolicies)
	}
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = '1' AND v3 = 'product:list'`); got != 1 {
		t.Fatalf("超管对 product:list 的既有策略应保持 1 条，实际 %d", got)
	}

	permission.Reset()
	permission.Declare("GET", "/api/test/widget/list2", permission.ProductList)

	fourth, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("第四次同步失败: %v", err)
	}
	if fourth.PathFixed != 1 {
		t.Fatalf("应修正 1 条权限点路径，实际 %d", fourth.PathFixed)
	}
	if got := strValue(t, db,
		`SELECT api_path FROM sys_permission WHERE permission_code = 'product:list'`); got != "/api/test/widget/list2" {
		t.Fatalf("权限点路径未跟随声明: %q", got)
	}

	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 = 'product:list'
			AND v1 = '/api/test/widget/list2'`); got != 2 {
		t.Fatalf("引用该权限点的策略行应全部对齐到新路径，实际 %d 条", got)
	}
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = 'editor' AND v3 = 'product:list'`); got != 1 {
		t.Fatalf("路径改写不应改动授权主体（editor 行丢失）")
	}

	if got := countRows(t, db,
		`SELECT count(*) FROM sys_permission WHERE permission_code = 'product:list' AND status = 0`); got != 1 {
		t.Fatalf("路径改写改动了人工设置的状态列")
	}
}

func TestDeclareAcceptsAnyPermCode(t *testing.T) {
	// 权限名称与存在性以数据库为准；代码侧只登记路由绑定，
	// 拼错的 code 由启动期对账（数据库目录 vs 已登记绑定）显式报错，不靠代码目录 panic。
	permission.Reset()
	defer permission.Reset()
	permission.Declare("GET", "/api/test/typo", permission.Perm("product:lst"))
	routes := permission.RoutesOf(permission.Perm("product:lst"))
	if len(routes) != 1 || routes[0].Path != "/api/test/typo" {
		t.Fatalf("登记应保留真实绑定，实际 %+v", routes)
	}
}

func TestSyncToDBRejectsDuplicateDeclaration(t *testing.T) {
	permission.Reset()
	defer func() {
		if recover() == nil {
			t.Fatal("同一路由重复声明权限点应当 panic")
		}
		permission.Reset()
	}()
	permission.Declare("GET", "/api/test/dup", permission.ProductList)
	permission.Declare("GET", "/api/test/dup", permission.ProductCreate)
}
