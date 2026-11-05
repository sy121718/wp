package migrations_test

import (
	"errors"
	"strings"
	"testing"

	casbinlib "github.com/casbin/casbin/v3"
	"gorm.io/gorm"

	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
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

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("补齐权限目录失败: %v", err)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("第二次跑种子失败: %v", err)
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

	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, bulkRoleCode, "批删测试角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, bulkRoleCode, singleDeletePath, singleDeleteCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, bulkRoleCode)

	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, onlyListRoleCode, "列表只读角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, '/api/block/list', 'GET', 'block:list')`, onlyListRoleCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, onlyListRoleCode)

	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 0)
		ON CONFLICT (role_code) DO UPDATE SET status = 0`, disabledRoleCode, "已禁用角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, disabledRoleCode, singleDeletePath, singleDeleteCode)

	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, nullRowLoanSubject, "NULL 行历史角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1)
		VALUES ('g2', ?, 'active')`, nullRowLoanSubject)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
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
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
		editor, bulkRoleCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
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

func blockPermExec(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("执行失败 %q: %v", query, err)
	}
}

const (
	markerMigrationVer = "596-sys-migration-marker"
	damagedRoleCode    = "biz_block_damaged_editor"
	newGrantRoleCode   = "biz_block_new_grant_role"
	revokeRoleCode     = "biz_block_revoke_role"
	dupSourceRoleCode  = "biz_block_dup_source_role"
	upgradeMemberName  = "biz_block_upgrade_member"
	listMemberName     = "biz_block_list_member"
)

func grantCopyMigrationSpec(t *testing.T) migrations.Migration {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == grantCopyMigration {
			return m
		}
	}
	t.Fatalf("迁移 %s 未注册：一次性升级复制没有出处", grantCopyMigration)
	return migrations.Migration{}
}

func markerMigrationSpec(t *testing.T) migrations.Migration {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == markerMigrationVer {
			return m
		}
	}
	t.Fatalf("迁移 %s 未注册：标记表没有出处", markerMigrationVer)
	return migrations.Migration{}
}

func runMigration(t *testing.T, db *gorm.DB, m migrations.Migration) error {
	t.Helper()
	return migrations.ApplyStatements(db, m.Version, m.TableName, m.CheckSQL, migrations.SplitStatements(m.SQL))
}

func runGrantCopy(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := runMigration(t, db, grantCopyMigrationSpec(t)); err != nil {
		t.Fatalf("执行 %s 失败: %v", grantCopyMigration, err)
	}
}

func resetGrantCopyMarker(t *testing.T, db *gorm.DB) {
	t.Helper()
	blockPermExec(t, db, `DELETE FROM sys_migration_marker WHERE version = ?`, grantCopyMigration)
}

func markerPresent(t *testing.T, db *gorm.DB, version string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM sys_migration_marker WHERE version = ?`, version).
		Scan(&n).Error; err != nil {
		t.Fatalf("查询标记失败: %v", err)
	}
	return n > 0
}

func liveRoleWithSingleDelete(t *testing.T, db *gorm.DB, code, path string) {
	t.Helper()
	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, code, "升级用例角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g2', ?, 'active')`, code)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, code, path, singleDeleteCode)
}

func countRulesOnPath(t *testing.T, db *gorm.DB, subject, path, code string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`, subject, path, code).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计 %s 的 %s 策略失败: %v", subject, code, err)
	}
	return n
}

func TestGrantCopyRunsOnceAndIsNotReplayedOnRestart(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	liveRoleWithSingleDelete(t, db, revokeRoleCode, singleDeletePath)
	resetGrantCopyMarker(t, db)
	runGrantCopy(t, db)
	if got := countRule(t, db, revokeRoleCode); got != 1 {
		t.Fatalf("升级时应复制 1 条批删授权，实际 %d 条", got)
	}
	if !markerPresent(t, db, grantCopyMigration) {
		t.Fatal("复制完成必须落执行标记（否则下次启动会重放）")
	}

	blockPermExec(t, db, `DELETE FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`, revokeRoleCode, bulkDeletePath, bulkDeleteCode)
	if got := countRule(t, db, revokeRoleCode); got != 0 {
		t.Fatalf("撤权后应剩 0 条批删授权，实际 %d 条", got)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("重启时执行迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重启时执行种子失败: %v", err)
	}
	runGrantCopy(t, db)
	if got := countRule(t, db, revokeRoleCode); got != 0 {
		t.Errorf("运维撤销的批删权被重跑授回（%d 条）—— 一次性复制退化成了每次启动重放", got)
	}

	liveRoleWithSingleDelete(t, db, newGrantRoleCode, singleDeletePath)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子失败: %v", err)
	}
	runGrantCopy(t, db)
	if got := countRule(t, db, newGrantRoleCode); got != 0 {
		t.Errorf("升级后新授的单删权被自动扩到批删（%d 条）—— 复制判据不能随当前授权变动", got)
	}

	if got := countRulesOnPath(t, db, newGrantRoleCode, singleDeletePath, singleDeleteCode); got != 1 {
		t.Fatalf("新授角色的单删权应存在（1 条），实际 %d 条", got)
	}
}

func TestGrantCopyConvergesDuplicateNullAndEmptySourceRows(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, dupSourceRoleCode, "重复源角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g2', ?, 'active')`, dupSourceRoleCode)

	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, NULL, NULL)`, dupSourceRoleCode, singleDeletePath, singleDeleteCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, '', '')`, dupSourceRoleCode, singleDeletePath, singleDeleteCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, NULL, NULL)`, dupSourceRoleCode, bulkDeletePath, singleDeleteCode)

	resetGrantCopyMarker(t, db)
	runGrantCopy(t, db)

	if got := countRule(t, db, dupSourceRoleCode); got != 1 {
		t.Errorf("三条等价源行只能复制出一条批删授权，实际 %d 条", got)
	}

	runGrantCopy(t, db)
	if got := countRule(t, db, dupSourceRoleCode); got != 1 {
		t.Errorf("重跑后仍应恰好 1 条，实际 %d 条", got)
	}
}

func TestGrantCopyRollsBackWithoutMarkerOnFailure(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)
	spec := grantCopyMigrationSpec(t)

	for _, seed := range migrations.AllSeeds() {
		if seed.Version == grantCopyMigration {
			t.Fatalf("%s 被注册成了 Seed：Seed 路径「判定 + Exec」没有事务包装，复制与标记无法同生共死", grantCopyMigration)
		}
	}

	liveRoleWithSingleDelete(t, db, dupSourceRoleCode, singleDeletePath)
	resetGrantCopyMarker(t, db)

	rollbackErr := db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range migrations.SplitStatements(spec.SQL) {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return errors.New("模拟复制中途失败")
	})
	if rollbackErr == nil {
		t.Fatal("事务应该失败（用例自身的前提）")
	}

	if markerPresent(t, db, grantCopyMigration) {
		t.Error("事务失败却留下了执行标记：下次启动会认为一次性动作已完成，那批授权永久缺失")
	}
	if got := countRule(t, db, dupSourceRoleCode); got != 0 {
		t.Errorf("事务失败不应留下半份复制，实际 %d 条", got)
	}

	runGrantCopy(t, db)
	if !markerPresent(t, db, grantCopyMigration) {
		t.Error("正常执行后应有标记")
	}
	if got := countRule(t, db, dupSourceRoleCode); got != 1 {
		t.Errorf("正常执行后应有 1 条批删授权，实际 %d 条", got)
	}
}

func TestFreshSchemaCreatesMarkerTableThenGrantCopy(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	blockPermExec(t, db, `DROP TABLE IF EXISTS sys_migration_marker`)

	if err := runMigration(t, db, markerMigrationSpec(t)); err != nil {
		t.Fatalf("全新 schema 上执行标记表迁移失败（CASE/to_regclass 兜不住的解析期错误会在这里暴露）: %v", err)
	}
	markerSpec := markerMigrationSpec(t)
	var exists bool
	if err := db.Raw(`SELECT to_regclass('sys_migration_marker') IS NOT NULL`).Scan(&exists).Error; err != nil {
		t.Fatalf("检查标记表失败: %v", err)
	}
	if !exists {
		t.Fatalf("%s 没有建出标记表", markerSpec.Version)
	}
	if err := runMigration(t, db, grantCopyMigrationSpec(t)); err != nil {
		t.Fatalf("标记表就绪后执行复制迁移失败: %v", err)
	}
	if !markerPresent(t, db, grantCopyMigration) {
		t.Error("全新 schema（零源主体）也必须落标记 —— 否则每次启动都要重新判断")
	}

	blockPermExec(t, db, `DELETE FROM sys_migration_marker WHERE version = ?`, grantCopyMigration)
	blockPermExec(t, db, `DROP TABLE IF EXISTS sys_migration_marker`)
	err := runMigration(t, db, grantCopyMigrationSpec(t))
	if err == nil {
		t.Fatal("标记表缺席时 597 应当报错（说明它的检查语句确实依赖该表存在）")
	}
	if !strings.Contains(err.Error(), "sys_migration_marker") {
		t.Errorf("报错应指向缺失的标记表，实际: %v", err)
	}
}

func TestUpgradeFromDamagedSharedCodeStateConverges(t *testing.T) {
	db, e := newBlockPermissionEnv(t)

	blockPermExec(t, db, `UPDATE sys_permission SET api_path = ?, api_method = 'POST' WHERE permission_code = ?`,
		bulkDeletePath, singleDeleteCode)
	liveRoleWithSingleDelete(t, db, damagedRoleCode, bulkDeletePath)

	blockPermExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, onlyListRoleCode, "列表只读角色")
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g2', ?, 'active')`, onlyListRoleCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, '/api/block/list', 'GET', 'block:list')`, onlyListRoleCode)

	resetGrantCopyMarker(t, db)

	if err := migrations.Run(db); err != nil {
		t.Fatalf("升级执行迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("升级执行种子失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("补齐权限目录失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}

	for code, wantPath := range map[string]string{
		singleDeleteCode: singleDeletePath,
		bulkDeleteCode:   bulkDeletePath,
	} {
		var gotPath string
		if err := db.Raw(`SELECT api_path FROM sys_permission WHERE permission_code = ?`, code).
			Row().Scan(&gotPath); err != nil {
			t.Fatalf("读取 %s 的路径失败: %v", code, err)
		}
		if gotPath != wantPath {
			t.Errorf("%s 应回到 %s，实际 %s", code, wantPath, gotPath)
		}
	}

	if got := countRulesOnPath(t, db, damagedRoleCode, singleDeletePath, singleDeleteCode); got != 1 {
		t.Errorf("受损主体的单删策略应被对齐回 canonical 路径，实际 %d 条", got)
	}

	if got := countRule(t, db, damagedRoleCode); got != 1 {
		t.Errorf("受损起点的历史主体应获得 1 条批删授权，实际 %d 条", got)
	}
	if got := countRule(t, db, onlyListRoleCode); got != 0 {
		t.Errorf("只读角色不得获得批删授权，实际 %d 条", got)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("第二次跑种子失败: %v", err)
	}

	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
		upgradeMemberName, damagedRoleCode)
	blockPermExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
		listMemberName, onlyListRoleCode)
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}
	for _, path := range []string{singleDeletePath, bulkDeletePath} {
		if !enforce(t, e, upgradeMemberName, path, "POST") {
			t.Errorf("升级后历史主体在 %s 上应通过（这条缺陷的本体就是它 403）", path)
		}
	}
	if !enforce(t, e, listMemberName, "/api/block/list", "GET") {
		t.Error("反例不成立：只读角色成员连列表都读不了，说明同步把人整个拒了（那样「被拒」就不是因为没批删权）")
	}
	for _, path := range []string{singleDeletePath, bulkDeletePath} {
		if enforce(t, e, listMemberName, path, "POST") {
			t.Errorf("只读角色成员在 %s 上不应通过（不得扩权）", path)
		}
	}
}

func TestBulkDeletePermissionSeedStaysSettledWhenDisabled(t *testing.T) {
	db, e := newBlockPermissionEnv(t)
	seed := blockBulkSeed(t)

	blockPermExec(t, db, `UPDATE sys_permission
		SET status = 0, permission_name = '运维停用的名称', module = 'ops'
		WHERE permission_code = ?`, bulkDeleteCode)

	var settled int64
	if err := db.Raw(seed.ConditionSQL).Scan(&settled).Error; err != nil {
		t.Fatalf("执行 594 的条件失败: %v", err)
	}
	if settled != 1 {
		t.Error("权限点被人工停用后条件仍应成立（>0 = 跳过）——否则这条 seed 每次启动都重放一遍")
	}

	runSeedSQL(t, db, seed)
	var status int64
	var name, module string
	if err := db.Raw(`SELECT status, permission_name, module FROM sys_permission WHERE permission_code = ?`, bulkDeleteCode).
		Row().Scan(&status, &name, &module); err != nil {
		t.Fatalf("回读权限点失败: %v", err)
	}
	if status != 0 {
		t.Errorf("迁移把运维停用的权限点重新启用了（status=%d）：人工列不许被改", status)
	}
	if name != "运维停用的名称" || module != "ops" {
		t.Errorf("迁移覆盖了人工列：name=%q module=%q", name, module)
	}

	var superID string
	if err := db.Raw(`SELECT CAST(id AS VARCHAR) FROM sys_admin WHERE is_admin = 1 AND status = 1 LIMIT 1`).
		Scan(&superID).Error; err != nil || superID == "" {
		t.Fatalf("取超管失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}
	if !enforce(t, e, superID, bulkDeletePath, "POST") {
		t.Error("权限点 status=0 时超管仍应通过判定（Casbin matcher 不读 sys_permission.status）")
	}
}

func TestBulkDeletePermissionLiteralsMatchDeclaredRoute(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	routePath, routeMethod := bulkDeletePath, "POST"

	var gotPath, gotMethod string
	if err := db.Raw(`SELECT api_path, api_method FROM sys_permission WHERE permission_code = ?`, bulkDeleteCode).
		Row().Scan(&gotPath, &gotMethod); err != nil {
		t.Fatalf("回读权限点失败: %v", err)
	}
	if gotPath != routePath || gotMethod != routeMethod {
		t.Errorf("权限点行 %s %s 与路由声明 %s %s 不一致", gotMethod, gotPath, routeMethod, routePath)
	}

	wantLiteral := "'" + routePath + "'"
	for _, m := range migrations.All() {

		if m.Version != grantCopyMigration {
			continue
		}
		if !strings.Contains(m.SQL, wantLiteral) {
			t.Errorf("迁移 %s 的 SQL 里没有路径字面量 %s（与路由声明漂移）", m.Version, wantLiteral)
		}
	}
	for _, s := range migrations.AllSeeds() {
		if s.Version != blockBulkSeedVer {
			continue
		}
		if !strings.Contains(s.SQL, wantLiteral) {
			t.Errorf("种子 %s 的 SQL 里没有路径字面量 %s（与路由声明漂移）", s.Version, wantLiteral)
		}
	}
}
