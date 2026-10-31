package permission_test

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

func runSeedSQL(t *testing.T, db *gorm.DB, seed migrations.Seed) {
	t.Helper()
	for _, stmt := range migrations.SplitStatements(seed.SQL) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行 %s 失败: %v", seed.Version, err)
		}
	}
}

func enforce(t *testing.T, e *casbinlib.SyncedEnforcer, sub, obj, act string) bool {
	t.Helper()
	ok, err := e.Enforce(sub, obj, act)
	if err != nil {
		t.Fatalf("Enforce(%s, %s, %s) 出错: %v", sub, obj, act, err)
	}
	return ok
}

func TestBlockDeletePathsStaySeparateAfterSync(t *testing.T) {
	db, e := newBlockPermissionEnv(t)
	ctx := context.Background()

	permission.Reset()
	permission.Declare("POST", singleDeletePath, permission.BlockDelete)
	permission.Declare("POST", bulkDeletePath, permission.BlockBulkDelete)

	if _, err := permission.SyncToDB(ctx, db); err != nil {
		t.Fatalf("SyncToDB 失败: %v", err)
	}

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

func TestBulkDeleteGrantCopyKeepsCapabilityWithoutExpanding(t *testing.T) {
	db, e := newBlockPermissionEnv(t)

	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, bulkRoleCode, "批删测试角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, bulkRoleCode, singleDeletePath, singleDeleteCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, bulkRoleCode)

	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, onlyListRoleCode, "列表只读角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, '/api/block/list', 'GET', 'block:list')`, onlyListRoleCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, onlyListRoleCode)

	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 0)
		ON CONFLICT (role_code) DO UPDATE SET status = 0`, disabledRoleCode, "已禁用角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, disabledRoleCode, singleDeletePath, singleDeleteCode)

	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, nullRowLoanSubject, "NULL 行历史角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, nullRowLoanSubject)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, NULL, NULL)`, nullRowLoanSubject, singleDeletePath, singleDeleteCode)

	resetGrantCopyMarker(t, db)
	runGrantCopy(t, db)

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

	if !enforce(t, e, viewer, "/api/block/list", "GET") {
		t.Error("只读角色成员应仍能读列表")
	}
}

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

func mustExec(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("执行失败 %q: %v", query, err)
	}
}
