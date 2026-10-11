package permission_test

// block_bulk_delete_test.go — 批删独立权限点的**真实库**回归：声明 → SyncToDB → Enforce。
//
// 为什么必须是真实库 + 真 Enforce：这条缺陷在纯字符串断言下完全隐形 ——
// `sys_permission` 每个权限点只有**一列** api_path/api_method，两条 API 路由共用一个 code 时
// （batch 删除曾经声明 block:delete），SyncToDB 只能把这两列对齐到其中一条，
// 另一条路径上的策略随即失配：超管点批删 403、角色静默失权，而代码、快照、门禁全绿。
//
// 覆盖四件事：
//  1. 一个权限点只绑一条 API 路径，且反复同步不会让两条路径互相顶；
//  2. 超管在**两条路径**上 Enforce 都通过（声明与同步的真实顺序：先 seed 后 SyncToDB）；
//  3. 迁移 597 把「原来持有 block:delete 的活主体」的授权**一次性**复制到批删权限点上
//     （能力保持），且**只**复制这些主体 —— 只持有 block:list 的角色、被禁用的角色都拿不到；
//  4. 复制的幂等性靠**执行标记**（sys_migration_marker 的行）而不是「目标行在不在」：
//     带 v4/v5=NULL 的历史行（Postgres 唯一索引对 NULL 不判等）也只能收敛成一条。
//     一次性语义（撤权不被授回、升级后新授单删不自动扩权）在 block_bulk_delete_upgrade_test.go。

import (
	"context"
	"testing"

	"go_wp/internal/permission"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	casbinlib "github.com/casbin/casbin/v3"
	"gorm.io/gorm"
)

const (
	bulkDeletePath     = "/api/block/bulk-delete"
	singleDeletePath   = "/api/block/delete"
	bulkDeleteCode     = "block:bulk_delete"
	singleDeleteCode   = "block:delete"
	blockBulkSeedVer   = "594-block-bulk-delete-permission"
	grantCopyMigration = "597-block-bulk-delete-grant-copy"
	bulkRoleCode       = "biz_block_bulk_editor"
	onlyListRoleCode   = "biz_block_list_viewer"
	disabledRoleCode   = "biz_block_disabled_editor"
	nullRowLoanSubject = "biz_block_null_row_role"
)

// newBlockPermissionEnv 建一套「迁移 + seed + Casbin」的真实环境。
//
// 顺序刻意与生产一致：先建超管账号，再跑 seed（030/031/594 都按 is_admin=1 逐行授策略），
// 最后初始化 Casbin（它从库里 LoadPolicy）。
func newBlockPermissionEnv(t *testing.T) (*gorm.DB, *casbinlib.SyncedEnforcer) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatalf("准备测试超管失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行业务权限 seed 失败: %v", err)
	}
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := pkgcasbin.Close(); err != nil {
			t.Errorf("关闭 Casbin 失败: %v", err)
		}
	})
	enforcer := pkgcasbin.GetEnforcer()
	if enforcer == nil {
		t.Fatal("Casbin enforcer 未初始化")
	}
	return db, enforcer
}

// blockBulkSeed 取注册表里的 594（长期定义：权限点行 + 超管策略补缺）。
func blockBulkSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == blockBulkSeedVer {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册：批删权限点的授权延续没有出处", blockBulkSeedVer)
	return migrations.Seed{}
}

// runSeedSQL 按生产同款方式执行一条 seed 的全部语句。
func runSeedSQL(t *testing.T, db *gorm.DB, seed migrations.Seed) {
	t.Helper()
	for _, stmt := range migrations.SplitStatements(seed.SQL) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行 %s 失败: %v", seed.Version, err)
		}
	}
}

// enforce 执行一次判定，出错即终止用例。
func enforce(t *testing.T, e *casbinlib.SyncedEnforcer, sub, obj, act string) bool {
	t.Helper()
	ok, err := e.Enforce(sub, obj, act)
	if err != nil {
		t.Fatalf("Enforce(%s, %s, %s) 出错: %v", sub, obj, act, err)
	}
	return ok
}

// TestBlockDeletePathsStaySeparateAfterSync 一个权限点只绑一条路径，反复同步也不互相顶。
func TestBlockDeletePathsStaySeparateAfterSync(t *testing.T) {
	db, e := newBlockPermissionEnv(t)
	ctx := context.Background()

	// 与 block_router.go 完全一致的声明（Reset 清掉其他包可能留下的声明）。
	permission.Reset()
	permission.Declare("POST", singleDeletePath, permission.BlockDelete)
	permission.Declare("POST", bulkDeletePath, permission.BlockBulkDelete)

	if _, err := permission.SyncToDB(ctx, db); err != nil {
		t.Fatalf("SyncToDB 失败: %v", err)
	}
	// 第二次同步必须无写入：若两条路径共用一个 code，这里会一直 PathFixed=1（来回顶）。
	second, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("第二次 SyncToDB 失败: %v", err)
	}
	if second.PathFixed != 0 || second.Inserted != 0 {
		t.Fatalf("第二条路径不应改写任何权限点路径: %+v", second)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}

	for code, wantPath := range map[string]string{
		singleDeleteCode: singleDeletePath,
		bulkDeleteCode:   bulkDeletePath,
	} {
		var gotPath, gotMethod string
		if err := db.Raw(`SELECT api_path, api_method FROM sys_permission WHERE permission_code = ?`, code).
			Row().Scan(&gotPath, &gotMethod); err != nil {
			t.Fatalf("读取 %s 的路径失败: %v", code, err)
		}
		if gotPath != wantPath || gotMethod != "POST" {
			t.Errorf("%s 应绑 %s POST，实际 %s %s", code, wantPath, gotMethod, gotPath)
		}
	}

	// 超管：两条路径都要通过（这条断言就是缺陷本体 —— 共用一个 code 时必有一条 403）。
	var superID string
	if err := db.Raw(`SELECT CAST(id AS VARCHAR) FROM sys_admin WHERE is_admin = 1 AND status = 1 LIMIT 1`).
		Scan(&superID).Error; err != nil {
		t.Fatalf("取超管 id 失败: %v", err)
	}
	if superID == "" {
		t.Fatal("库里没有启用的超管：030/031/594 的超管策略无从验证")
	}
	for _, path := range []string{singleDeletePath, bulkDeletePath} {
		if !enforce(t, e, superID, path, "POST") {
			t.Errorf("超管在 %s 上应通过判定（权限点路径与策略必须一一对应）", path)
		}
	}
}

// TestBulkDeleteGrantCopyKeepsCapabilityWithoutExpanding 594 只搬「原来就有单删权」的主体。
func TestBulkDeleteGrantCopyKeepsCapabilityWithoutExpanding(t *testing.T) {
	db, e := newBlockPermissionEnv(t)

	// —— 三种主体，都先按「升级前」的状态摆好（批删行一律不存在）——
	// 1) 启用且带 g2 的角色，原来只持有单条删除 → 必须拿到批删权（能力保持）
	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, bulkRoleCode, "批删测试角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, bulkRoleCode, singleDeletePath, singleDeleteCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, bulkRoleCode)
	// 2) 启用的角色，只持有列表读权限 → 不得多出批删权（不扩权）
	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, onlyListRoleCode, "列表只读角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, '/api/block/list', 'GET', 'block:list')`, onlyListRoleCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, onlyListRoleCode)
	// 3) 被禁用的角色（status=0 且没有 g2 标记），历史上有单删授权 → 不搬
	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 0)
		ON CONFLICT (role_code) DO UPDATE SET status = 0`, disabledRoleCode, "已禁用角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, disabledRoleCode, singleDeletePath, singleDeleteCode)
	// 4) 启用角色 + v4/v5 为 NULL 的历史授权行（唯一索引对 NULL 不判等 —— 幂等陷阱的样本）。
	//    主体本身必须是「活着的」，否则它走不到复制这一步，测的就不是 NULL 陷阱。
	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, nullRowLoanSubject, "NULL 行历史角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, nullRowLoanSubject)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, NULL, NULL)`, nullRowLoanSubject, singleDeletePath, singleDeleteCode)

	// 升级那一刻：清掉执行标记（= 一次性动作尚未做过），再走生产同款路径执行 597。
	resetGrantCopyMarker(t, db)
	runGrantCopy(t, db)
	// 再来一次：标记已在 → CheckSQL 命中、整条跳过。重复行的兜底不再承担幂等（它只管
	// 「同一主体有多条源行」），一次性语义由标记承担。
	runGrantCopy(t, db)

	if got := countRule(t, db, bulkRoleCode); got != 1 {
		t.Errorf("持有单删权的启用角色应被复制 1 条批删授权，实际 %d 条（重复=幂等判据漏了 v4/v5）", got)
	}
	if got := countRule(t, db, onlyListRoleCode); got != 0 {
		t.Errorf("只持有列表读权限的角色不得获得批删权，实际 %d 条", got)
	}
	if got := countRule(t, db, disabledRoleCode); got != 0 {
		t.Errorf("禁用角色的历史授权不应被复制，实际 %d 条", got)
	}
	if got := countRule(t, db, nullRowLoanSubject); got != 1 {
		t.Errorf("v4/v5 为 NULL 的历史行应恰好被补 1 条，实际 %d 条", got)
	}

	// 判定层复核：角色成员「刚好能删单条 + 批删」，列表只读角色两条都拒。
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}
	editor, viewer := "biz_block_bulk_member", "biz_block_list_member"
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
		editor, bulkRoleCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
		viewer, onlyListRoleCode)
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}
	for _, path := range []string{singleDeletePath, bulkDeletePath} {
		if !enforce(t, e, editor, path, "POST") {
			t.Errorf("持有单删权的角色成员在 %s 上应通过（升级不该丢能力）", path)
		}
		if enforce(t, e, viewer, path, "POST") {
			t.Errorf("只读角色成员在 %s 上不应通过（不得扩权）", path)
		}
	}
	// 只读角色成员仍应能读列表 —— 反例不能靠「把人整个拒掉」蒙对。
	if !enforce(t, e, viewer, "/api/block/list", "GET") {
		t.Error("只读角色成员应仍能读列表")
	}
}

// countRule 数某个主体名下的批删策略行。
func countRule(t *testing.T, db *gorm.DB, subject string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v3 = ? AND v1 = ?`, subject, bulkDeleteCode, bulkDeletePath).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计 %s 的批删策略失败: %v", subject, err)
	}
	return n
}

// mustExec 执行一条语句，失败即终止用例。
func mustExec(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("执行失败 %q: %v", query, err)
	}
}
