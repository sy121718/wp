package permission_test

// block_bulk_delete_upgrade_test.go — 一次性升级复制的**生命周期**回归（597 + 594 + SyncToDB）。
//
// 这一组用例守的是「一次性的语义」，它无法被单条 SQL 的幂等性覆盖：
//   A. 撤权之后重跑（重启 / RunSeeds / RunMigrations）不得把批删授回；
//   B. 升级**之后**新授的单删不得被自动扩到批删；
//   C. 同一主体的多条源行（v4/v5 = NULL 与空串两种历史形态）只能收敛成一条;
//   D. 复制与标记同事务：中途失败不留标记、也不留半份复制；
//   E. 全新 schema（标记表还不存在）必须能按 596 → 597 的顺序建表并执行；
//   F. 真实受损起点（block:delete 停在 bulk 路径）按完整生产顺序 migration → seed → sync 收口；
//   G. 594 的注册条件在运维停用权限点后仍然成立（不永久重放），且 SQL 不改人工列。
//
// 顺序事实（决定了这一组怎么写）：production 的 routers/assembly 先 `migrations.Run`（全部 Migration，
// 含 596/597），再 `migrations.RunSeeds`（594 才建权限点行），最后 `SyncToDB`。所以 597 执行时
// **目标权限点行可能还不存在** —— 用例不能假定 594 先落，必须跑完整序列。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/permission"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"

	"gorm.io/gorm"
)

const (
	markerMigrationVer = "596-sys-migration-marker"
	damagedRoleCode    = "biz_block_damaged_editor"
	newGrantRoleCode   = "biz_block_new_grant_role"
	revokeRoleCode     = "biz_block_revoke_role"
	dupSourceRoleCode  = "biz_block_dup_source_role"
	upgradeMemberName  = "biz_block_upgrade_member"
	listMemberName     = "biz_block_list_member"
)

// grantCopyMigrationSpec 取注册表里的 597。
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

// markerMigrationSpec 取注册表里的 596（标记表结构）。
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

// runMigration 按生产同款路径执行一条迁移：ApplyStatements = 检查 + 全部语句同一事务。
func runMigration(t *testing.T, db *gorm.DB, m migrations.Migration) error {
	t.Helper()
	return migrations.ApplyStatements(db, m.Version, m.TableName, m.CheckSQL, migrations.SplitStatements(m.SQL))
}

// runGrantCopy 执行 597（失败即终止用例）。
func runGrantCopy(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := runMigration(t, db, grantCopyMigrationSpec(t)); err != nil {
		t.Fatalf("执行 %s 失败: %v", grantCopyMigration, err)
	}
}

// resetGrantCopyMarker 把环境还原成「升级那一刻」：标记不存在 = 一次性动作还没做过。
//
// 之所以需要它：测试库是全新迁移过的库，标记在迁移阶段就已经落了（零源主体也落标记）。
// 要验证「复制这一下」，就得先把标记拿掉一次。
func resetGrantCopyMarker(t *testing.T, db *gorm.DB) {
	t.Helper()
	mustExec(t, db, `DELETE FROM sys_migration_marker WHERE version = ?`, grantCopyMigration)
}

// markerPresent 标记行是否存在。
func markerPresent(t *testing.T, db *gorm.DB, version string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM sys_migration_marker WHERE version = ?`, version).
		Scan(&n).Error; err != nil {
		t.Fatalf("查询标记失败: %v", err)
	}
	return n > 0
}

// liveRoleWithSingleDelete 造一个「活着、有 g2、历史上持有单删」的角色，返回角色码。
func liveRoleWithSingleDelete(t *testing.T, db *gorm.DB, code, path string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, code, "升级用例角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g2', ?, 'active')`, code)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)`, code, path, singleDeleteCode)
}

// countRulesOnPath 数某主体在指定路径上的策略行（路径也是判据的一部分：只看 v3 会把「同步把路径
// 对齐前后各插一条」这种问题掩盖掉）。
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

// A+B. 一次性语义：撤权不被授回、升级后新授的单删不自动扩权。
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

	// 运维撤掉这个角色的批删权（只删批删策略行，单删保留）。
	mustExec(t, db, `DELETE FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`, revokeRoleCode, bulkDeletePath, bulkDeleteCode)
	if got := countRule(t, db, revokeRoleCode); got != 0 {
		t.Fatalf("撤权后应剩 0 条批删授权，实际 %d 条", got)
	}

	// 模拟一次完整重启：迁移 + 种子（生产顺序）。
	if err := migrations.Run(db); err != nil {
		t.Fatalf("重启时执行迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重启时执行种子失败: %v", err)
	}
	runGrantCopy(t, db) // 再显式跑一遍复制本身
	if got := countRule(t, db, revokeRoleCode); got != 0 {
		t.Errorf("运维撤销的批删权被重跑授回（%d 条）—— 一次性复制退化成了每次启动重放", got)
	}

	// 升级**之后**新授的单删权：不允许被自动扩到批删（那是没人审批过的权限）。
	liveRoleWithSingleDelete(t, db, newGrantRoleCode, singleDeletePath)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子失败: %v", err)
	}
	runGrantCopy(t, db)
	if got := countRule(t, db, newGrantRoleCode); got != 0 {
		t.Errorf("升级后新授的单删权被自动扩到批删（%d 条）—— 复制判据不能随当前授权变动", got)
	}
	// 反例不能靠「谁都没授权」蒙对：这个新角色的单删权必须真的在库里。
	if got := countRulesOnPath(t, db, newGrantRoleCode, singleDeletePath, singleDeleteCode); got != 1 {
		t.Fatalf("新授角色的单删权应存在（1 条），实际 %d 条", got)
	}
}

// C. 同一主体的多条源行（NULL 与空串两种历史形态 + 两条历史路径）收敛成一条。
func TestGrantCopyConvergesDuplicateNullAndEmptySourceRows(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, dupSourceRoleCode, "重复源角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g2', ?, 'active')`, dupSourceRoleCode)
	// 三条等价的历史源行：canonical 路径（v4/v5 NULL）、canonical 路径（v4/v5 空串）、bulk 路径（NULL）。
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, NULL, NULL)`, dupSourceRoleCode, singleDeletePath, singleDeleteCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, '', '')`, dupSourceRoleCode, singleDeletePath, singleDeleteCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
		VALUES ('p', ?, ?, 'POST', ?, NULL, NULL)`, dupSourceRoleCode, bulkDeletePath, singleDeleteCode)

	resetGrantCopyMarker(t, db)
	runGrantCopy(t, db)

	if got := countRule(t, db, dupSourceRoleCode); got != 1 {
		t.Errorf("三条等价源行只能复制出一条批删授权，实际 %d 条", got)
	}
	// 再跑一遍（标记在 → 跳过）也不该多出第二行。
	runGrantCopy(t, db)
	if got := countRule(t, db, dupSourceRoleCode); got != 1 {
		t.Errorf("重跑后仍应恰好 1 条，实际 %d 条", got)
	}
}

// D. 复制与标记同事务：中途失败不留标记、也不留半份复制。
func TestGrantCopyRollsBackWithoutMarkerOnFailure(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)
	spec := grantCopyMigrationSpec(t)

	// 结构性前提：这条版本必须是 Migration（Seed 路径没有事务包装，原子性无从谈起）。
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == grantCopyMigration {
			t.Fatalf("%s 被注册成了 Seed：Seed 路径「判定 + Exec」没有事务包装，复制与标记无法同生共死", grantCopyMigration)
		}
	}

	liveRoleWithSingleDelete(t, db, dupSourceRoleCode, singleDeletePath)
	resetGrantCopyMarker(t, db)

	// 用同一条迁移的全部语句跑一个**注定失败**的事务：语句照原样执行，最后主动报错。
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

	// 反过来：正常执行一次后，标记与复制都在（证明上面的回滚不是「什么都没发生过」）。
	runGrantCopy(t, db)
	if !markerPresent(t, db, grantCopyMigration) {
		t.Error("正常执行后应有标记")
	}
	if got := countRule(t, db, dupSourceRoleCode); got != 1 {
		t.Errorf("正常执行后应有 1 条批删授权，实际 %d 条", got)
	}
}

// E. 全新 schema：标记表还不存在时，必须按 596 → 597 的顺序建表并执行。
//
// 这条用例直接钉住审核发现的阻断：最初「建表 + 查标记行」写在同一条迁移的 CheckSQL 里，
// PostgreSQL 在**解析期**就解析整条 SQL 的 FROM 关系（CASE 短路只在执行期），
// 于是全新库里 `relation "sys_migration_marker" does not exist` —— 迁移跑不起来。
func TestFreshSchemaCreatesMarkerTableThenGrantCopy(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	// 回到「全新 schema」：把标记表整个删掉（连同已落的一次性标记）。
	mustExec(t, db, `DROP TABLE IF EXISTS sys_migration_marker`)

	// 先 596（建表），再 597（复制 + 标记）—— 与迁移按版本排序执行的真实顺序一致。
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

	// 再验一次反面：没有 596 的表时，597 的检查必然报「关系不存在」—— 这就是必须拆成两条的
	// 原因。失败发生在事务里，所以不会留下任何东西（下面两条断言同时钉住这一点）。
	mustExec(t, db, `DELETE FROM sys_migration_marker WHERE version = ?`, grantCopyMigration)
	mustExec(t, db, `DROP TABLE IF EXISTS sys_migration_marker`)
	err := runMigration(t, db, grantCopyMigrationSpec(t))
	if err == nil {
		t.Fatal("标记表缺席时 597 应当报错（说明它的检查语句确实依赖该表存在）")
	}
	if !strings.Contains(err.Error(), "sys_migration_marker") {
		t.Errorf("报错应指向缺失的标记表，实际: %v", err)
	}
}

// F. 真实受损起点：block:delete 停在 bulk 路径 → 完整生产顺序一次性收口。
func TestUpgradeFromDamagedSharedCodeStateConverges(t *testing.T) {
	db, e := newBlockPermissionEnv(t)
	ctx := context.Background()

	// —— 造升级前的受损状态 ——
	// 双码合并期 SyncToDB 的 UPDATE … FROM 在同一 code 的两条路径里挑了 bulk 那条，
	// 于是权限点行与它名下的策略行都停在 /api/block/bulk-delete。
	mustExec(t, db, `UPDATE sys_permission SET api_path = ?, api_method = 'POST' WHERE permission_code = ?`,
		bulkDeletePath, singleDeleteCode)
	liveRoleWithSingleDelete(t, db, damagedRoleCode, bulkDeletePath)
	// 只读角色作为反例（不能被顺手扩权）。
	mustExec(t, db, `INSERT INTO sys_role (role_code, role_name, status) VALUES (?, ?, 1)
		ON CONFLICT (role_code) DO UPDATE SET status = 1`, onlyListRoleCode, "列表只读角色")
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g2', ?, 'active')`, onlyListRoleCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, '/api/block/list', 'GET', 'block:list')`, onlyListRoleCode)

	// 升级那一刻：标记还没落。
	resetGrantCopyMarker(t, db)

	// —— 完整生产顺序：migration → seed → sync ——
	if err := migrations.Run(db); err != nil {
		t.Fatalf("升级执行迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("升级执行种子失败: %v", err)
	}
	permission.Reset()
	permission.Declare("POST", singleDeletePath, permission.BlockDelete)
	permission.Declare("POST", bulkDeletePath, permission.BlockBulkDelete)
	if _, err := permission.SyncToDB(ctx, db); err != nil {
		t.Fatalf("升级同步权限失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载策略失败: %v", err)
	}

	// 1) 受损的 canonical 路径被 SyncToDB 修回；批删权限点绑自己的路径。
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
	// 2) 历史主体的单删策略行也被对齐回 canonical（否则判定层仍然只认 bulk 路径）。
	if got := countRulesOnPath(t, db, damagedRoleCode, singleDeletePath, singleDeleteCode); got != 1 {
		t.Errorf("受损主体的单删策略应被对齐回 canonical 路径，实际 %d 条", got)
	}
	// 3) 复制补齐了批删能力（源是「停在 bulk 路径」的那些行 —— 只认 canonical 就会漏掉整整一代）。
	if got := countRule(t, db, damagedRoleCode); got != 1 {
		t.Errorf("受损起点的历史主体应获得 1 条批删授权，实际 %d 条", got)
	}
	if got := countRule(t, db, onlyListRoleCode); got != 0 {
		t.Errorf("只读角色不得获得批删授权，实际 %d 条", got)
	}
	// 4) 再次同步必须无写入（受损状态已被完全收口，不该来回顶）。
	second, err := permission.SyncToDB(ctx, db)
	if err != nil {
		t.Fatalf("第二次同步失败: %v", err)
	}
	if second.PathFixed != 0 || second.Inserted != 0 {
		t.Errorf("收口后第二次同步不应写入: %+v", second)
	}
	// 5) 判定层复核：角色成员在**两条路径**都通过；只读角色两条都拒、但列表读仍然可以。
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
		upgradeMemberName, damagedRoleCode)
	mustExec(t, db, `INSERT INTO sys_casbin_rule (ptype, v0, v1) VALUES ('g', ?, ?)`,
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

// G. 594 的注册条件在运维停用权限点后仍成立（不永久重放），且 SQL 不改人工列。
func TestBulkDeletePermissionSeedStaysSettledWhenDisabled(t *testing.T) {
	db, e := newBlockPermissionEnv(t)
	seed := blockBulkSeed(t)

	// 运维改的三个人工列：status / permission_name / module。
	mustExec(t, db, `UPDATE sys_permission
		SET status = 0, permission_name = '运维停用的名称', module = 'ops'
		WHERE permission_code = ?`, bulkDeleteCode)

	var settled int64
	if err := db.Raw(seed.ConditionSQL).Scan(&settled).Error; err != nil {
		t.Fatalf("执行 594 的条件失败: %v", err)
	}
	if settled != 1 {
		t.Error("权限点被人工停用后条件仍应成立（>0 = 跳过）——否则这条 seed 每次启动都重放一遍")
	}

	// 即使强制把这批语句重跑一遍，人工列也必须纹丝不动。
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
	// 这份断言同时说明 597 为什么「目标停用也照做复制」：判定层不读 status，
	// 停用权限点并不会把通道关掉（真要不给，得删策略行）。
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

// H. 594/597 里的路径字面量与真实路由声明一致（seed 与路由漂移 = 迁移补的策略指向一条不存在的路径）。
func TestBulkDeletePermissionLiteralsMatchDeclaredRoute(t *testing.T) {
	db, _ := newBlockPermissionEnv(t)

	permission.Reset()
	permission.Declare("POST", bulkDeletePath, permission.BlockBulkDelete)
	routes := permission.RoutesOf(permission.BlockBulkDelete)
	if len(routes) != 1 {
		t.Fatalf("block:bulk_delete 应恰好声明 1 条路由，实际 %d 条", len(routes))
	}
	if routes[0].Path != bulkDeletePath || routes[0].Method != "POST" {
		t.Fatalf("声明与测试常量不一致：%s %s", routes[0].Method, routes[0].Path)
	}

	// 权限点行（594 落的）必须与声明一致。
	var gotPath, gotMethod string
	if err := db.Raw(`SELECT api_path, api_method FROM sys_permission WHERE permission_code = ?`, bulkDeleteCode).
		Row().Scan(&gotPath, &gotMethod); err != nil {
		t.Fatalf("回读权限点失败: %v", err)
	}
	if gotPath != routes[0].Path || gotMethod != routes[0].Method {
		t.Errorf("权限点行 %s %s 与路由声明 %s %s 不一致", gotMethod, gotPath, routes[0].Method, routes[0].Path)
	}

	// 594/597 的 SQL 文本里也必须出现同一条路径（否则迁移补的策略指向别的地址）。
	// 以注册表为准读取，而不是直接读文件 —— 读到的就是生产真正执行的那份文本。
	wantLiteral := "'" + routes[0].Path + "'"
	for _, m := range migrations.All() {
		// 596 只建标记表、不含权限路径，所以只比对真正写策略的 597（与 594 的 seed SQL）。
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
