package permission_test

// sync_test.go — 启动期权限点同步的行为验证（审计 SEC-011 的「幂等且不覆盖人工授权」）。
//
// 要证明的四件事：
//  1. 补缺：声明了的权限点与超管策略会被写库；
//  2. 幂等：重复 SyncToDB 不再产生任何写入；
//  3. 不覆盖人工数据：人工改过的 permission_name 保留、人工禁用的 status 保持 0、
//     角色策略行不被删除、被人工删掉的超管策略会被补回（只补缺失，不重写已有）；
//  4. 路径改写只对齐「路由事实列」：api_path/api_method 跟随声明，引用该 code 的策略行
//     v1/v2 跟随，而**授权主体 v0 与权限点代码 v3 不变**（没有撤谁的权，也没有多授一条）。
//
// 用**外部测试包**（permission_test）：public/test/support 经 test_bootstrap 依赖
// internal/routers，而 routers 依赖本包 —— 包内测试 import support 会构成 import 环。

import (
	"context"
	"testing"

	"go_wp/internal/permission"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// countRows 执行一条 count 查询并返回结果，失败即终止用例。
func countRows(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("查询失败 %q: %v", query, err)
	}
	return n
}

// strValue 取一个字符串列，失败即终止用例。
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

	// 超管账号：超管策略补全（sys_admin.is_admin=1）的授权对象。
	if err := db.Exec(`INSERT INTO sys_admin (id, username, password, status, is_admin)
		VALUES (1, 'sync_admin', 'x', 1, 1) ON CONFLICT (id) DO NOTHING`).Error; err != nil {
		t.Fatalf("准备超管账号失败: %v", err)
	}

	// 起点归零：本用例用独立 schema，清空权限点与策略表，让「补了多少」可精确断言
	//（生产库中这些行由历史 seed 与声明式同步共同维护，绝对值不可作为断言依据）。
	if err := db.Exec("DELETE FROM sys_casbin_rule").Error; err != nil {
		t.Fatalf("清空策略表失败: %v", err)
	}
	if err := db.Exec("DELETE FROM sys_permission").Error; err != nil {
		t.Fatalf("清空权限点表失败: %v", err)
	}

	// 声明两条权限点（复用真实常量：声明表只关心 code → name/module 元数据）。
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

	// 幂等：再跑一次，什么都不写。
	second, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("第二次同步失败: %v", err)
	}
	if second.Inserted != 0 || second.PathFixed != 0 || second.PolicyFixed != 0 || second.SuperPolicies != 0 {
		t.Fatalf("第二次同步应完全幂等，实际 %s", second.String())
	}

	// —— 人工数据：改名 + 禁用 + 角色授权 + 删掉一条超管策略 ——
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
	// 人工改名不被覆盖。
	if got := strValue(t, db,
		`SELECT permission_name FROM sys_permission WHERE permission_code = 'product:list'`); got != "人工改名" {
		t.Fatalf("人工改过的权限点名称被覆盖: %q", got)
	}
	// 人工禁用（status=0）不被复活。
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_permission WHERE permission_code = 'product:list' AND status = 1`); got != 0 {
		t.Fatalf("被人工禁用的权限点被自动复用了")
	}
	// 角色策略行不被删除。
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = 'editor'`); got != 1 {
		t.Fatalf("人工角色授权被删除，剩余 %d 行", got)
	}
	// 被删掉的超管策略按「缺失才补」补回 1 条（product:list 已禁用，不再补）。
	if third.SuperPolicies != 1 {
		t.Fatalf("应补回 1 条超管策略，实际 %d", third.SuperPolicies)
	}
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = '1' AND v3 = 'product:list'`); got != 1 {
		t.Fatalf("超管对 product:list 的既有策略应保持 1 条，实际 %d", got)
	}

	// —— 路径改写：只对齐路由事实列，不动授权主体 ——
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
	// 两条策略行（超管 + editor 角色）的路径同步跟随，授权主体与 code 不变。
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v3 = 'product:list'
			AND v1 = '/api/test/widget/list2'`); got != 2 {
		t.Fatalf("引用该权限点的策略行应全部对齐到新路径，实际 %d 条", got)
	}
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_casbin_rule WHERE ptype = 'p' AND v0 = 'editor' AND v3 = 'product:list'`); got != 1 {
		t.Fatalf("路径改写不应改动授权主体（editor 行丢失）")
	}
	// product:list 仍处于人工禁用状态：路径修正不复活它。
	if got := countRows(t, db,
		`SELECT count(*) FROM sys_permission WHERE permission_code = 'product:list' AND status = 0`); got != 1 {
		t.Fatalf("路径改写改动了人工设置的状态列")
	}
}

// TestSyncToDBRejectsUnknownPerm 未登记常量（拼错的字面量）必须当场炸掉，而不是安静建一个权限点。
func TestSyncToDBRejectsUnknownPerm(t *testing.T) {
	permission.Reset()
	defer func() {
		if recover() == nil {
			t.Fatal("未登记的权限点代码应当 panic（否则拼错的 code 会静默入库）")
		}
		permission.Reset()
	}()
	permission.Declare("GET", "/api/test/typo", permission.Perm("product:lst"))
}

// TestSyncToDBRejectsDuplicateDeclaration 同一路由声明两次权限点必须炸掉。
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
