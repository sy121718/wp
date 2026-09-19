// tx_rollback_test.go — admin 三处写路径的事务回滚证明（2026-09-19 全模块事务审计：高 3 条）。
//
// 为什么用**数据库触发器**注入故障，而不是给生产代码开测试开关：
//
//	要证明的是「策略行 / 子孙行的写入失败时业务行会回滚」，而这条链路的中间环节全在
//	PostgreSQL 里（同一个 *gorm.DB 事务）。在库侧让第 N 步抛异常，走的是**完全真实**的
//	代码路径（service → model → gorm → PG），不在生产代码上留测试后门，
//	也不会因为「开关忘了关」而让真实路径与测试路径分叉。
//
// 断言方向一律是「业务行保持原值」：
//   - 权限点：sys_permission 的 name/module/api_path/api_method/status 全都没变，且旧策略行还在
//     （审计描述的形态是「策略已删、Add 中途失败 → 该权限点连带超管全员 403」）；
//   - 角色禁用：sys_role.status 仍是启用（审计形态是「库里已禁用、g2 没删掉 → 仍被放行」）；
//   - 角色新建：sys_role 里没有这一行；
//   - 部门移动：自身 parent_id/ancestors 与在改字段全部保持原值，子孙祖先链也没变。
package unit

import (
	"context"
	"fmt"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

// installPolicyFailTrigger 在 sys_casbin_rule 上装一个「命中 whereExpr 就抛异常」的触发器。
// event 取 INSERT / DELETE；whenExpr 里可用 NEW（INSERT）或 OLD（DELETE）。
func installPolicyFailTrigger(t *testing.T, e *env, event, whenExpr string) {
	t.Helper()
	installFailTrigger(t, e, "sys_casbin_rule", event, whenExpr)
}

// installFailTrigger 通用故障注入：BEFORE <event> 触发器，命中 whenExpr 即抛异常。
// 函数与触发器名都带随机后缀，只在本用例的 schema 内生效（每个用例一个新 schema）。
func installFailTrigger(t *testing.T, e *env, table, event, whenExpr string) {
	t.Helper()
	suffix := uniq("fault")
	fnName := "test_fail_" + suffix
	trgName := "trg_test_fail_" + suffix

	fnSQL := fmt.Sprintf("CREATE FUNCTION %s() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION '故障注入：写入失败'; END $$ LANGUAGE plpgsql", fnName)
	if err := e.db.Exec(fnSQL).Error; err != nil {
		t.Fatalf("创建故障注入函数失败: %v", err)
	}
	trgSQL := fmt.Sprintf("CREATE TRIGGER %s BEFORE %s ON %s FOR EACH ROW WHEN (%s) EXECUTE FUNCTION %s()",
		trgName, event, table, whenExpr, fnName)
	if err := e.db.Exec(trgSQL).Error; err != nil {
		t.Fatalf("创建故障注入触发器失败: %v", err)
	}
	t.Cleanup(func() {
		_ = e.db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s", trgName, table)).Error
		_ = e.db.Exec(fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", fnName)).Error
	})
}

// mustPermRow 读取权限点当前落库值。
func mustPermRow(t *testing.T, e *env, id uint64) adminmodel.PermissionEntity {
	t.Helper()
	var perm adminmodel.PermissionEntity
	if err := e.db.First(&perm, id).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	return perm
}

// TestPermUpdatePolicyFailureRollsBackPermissionRow 策略行写入失败时，sys_permission 行整体回滚。
//
// 复现审计形态：改一个**已分配**权限点的 api_path（definitionChanged && assigned），
// 策略替换里「旧 p 行已删、新 p 行写入失败」——修复前权限点的新定义已经落库，
// 而该权限点的策略行全没了（连带超管全员 403）；修复后两处写同事务整体回滚：
// 权限点保持旧定义，旧策略行也还在。
func TestPermUpdatePolicyFailureRollsBackPermissionRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_txfail:" + uniq("")
	const roleCode = "txfail_role"
	permID := createPerm(t, e, code, "/api/tx_old")
	if err := pkgcasbin.ReplaceRolePermissions(roleCode, [][3]string{{"/api/tx_old", "GET", code}}); err != nil {
		t.Fatalf("写入 Casbin 分配失败: %v", err)
	}
	before := mustPermRow(t, e, permID)

	// 故障注入：该权限点的新策略行写不进去（在真实的写入语句上抛异常）
	installPolicyFailTrigger(t, e, "INSERT",
		fmt.Sprintf("NEW.ptype = 'p' AND NEW.v3 = '%s'", code))

	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "改名后", Module: "console",
		APIPath: "/api/tx_new", APIMethod: "POST", Status: 1,
	})
	wantErr(t, err, "回滚") // 错误必须回给调用方，不允许静默吞掉

	// 1) 权限点行必须停在原值（name / module / path / method / status 全没变）
	after := mustPermRow(t, e, permID)
	if after.PermissionName != before.PermissionName || after.Module != before.Module ||
		after.APIPath != before.APIPath || after.APIMethod != before.APIMethod ||
		after.Status != before.Status {
		t.Fatalf("策略写失败时权限点行应整体回滚：before=%+v after=%+v", before, after)
	}
	if after.APIPath != "/api/tx_old" || after.APIMethod != "GET" {
		t.Fatalf("权限点定义应保持旧值: %+v", after)
	}

	// 2) 旧策略行必须还在（删除是同事务的一部分，一并回滚）
	perms, err := pkgcasbin.GetRolePermissions(roleCode)
	wantErr(t, err, "")
	if len(perms) != 1 || perms[0][0] != "/api/tx_old" || perms[0][1] != "GET" {
		t.Fatalf("回滚后策略应保持旧定义: %v", perms)
	}
	var policyRows int64
	if err := e.db.Table("sys_casbin_rule").
		Where("ptype = 'p' AND v3 = ?", code).Count(&policyRows).Error; err != nil {
		t.Fatalf("统计策略行失败: %v", err)
	}
	if policyRows != 1 {
		t.Fatalf("回滚后该权限点应仍恰好一行旧策略: got=%d", policyRows)
	}
}

// TestPermUpdatePolicyWriteCommitsWithRow 正向对照：策略写成功时两处一起提交。
func TestPermUpdatePolicyWriteCommitsWithRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_txok:" + uniq("")
	const roleCode = "txok_role"
	permID := createPerm(t, e, code, "/api/ok_old")
	if err := pkgcasbin.ReplaceRolePermissions(roleCode, [][3]string{{"/api/ok_old", "GET", code}}); err != nil {
		t.Fatalf("写入 Casbin 分配失败: %v", err)
	}

	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "新名", Module: "admin",
		APIPath: "/api/ok_new", APIMethod: "POST", Status: 1,
	})
	wantErr(t, err, "")

	after := mustPermRow(t, e, permID)
	if after.APIPath != "/api/ok_new" || after.APIMethod != "POST" {
		t.Fatalf("权限点新定义未落库: %+v", after)
	}
	perms, err := pkgcasbin.GetRolePermissions(roleCode)
	wantErr(t, err, "")
	if len(perms) != 1 || perms[0][0] != "/api/ok_new" || perms[0][1] != "POST" {
		t.Fatalf("权限策略未随权限点一起提交: %v", perms)
	}
	// 内存副本（Enforce 读的那一份）也必须已刷新
	if _, hit := pkgcasbin.GetCodeByURL("/api/ok_new", "POST"); !hit {
		t.Fatalf("提交后内存 URL→code 映射未刷新")
	}
}

// TestRoleUpdateG2DeleteFailureRollsBackStatus 禁用角色时 g2 删除失败 → sys_role.status 回滚为启用。
//
// 这是审计里最危险的一条：修复前先提交 status=0，再删 g2；g2 删除失败就留下
// 「库里已禁用、g2 还在」——该角色下所有人仍被放行，禁用等于没生效，报错只回给这一次请求，
// 运维从库里看不出异常。修复后整体回滚：禁用要么完整生效、要么完全不生效。
func TestRoleUpdateG2DeleteFailureRollsBackStatus(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_txfail_" + uniq("")
	roleID := createRole(t, e, code, "故障注入角色")

	// 故障注入：删该角色的 g2 行时抛异常
	installPolicyFailTrigger(t, e, "DELETE",
		fmt.Sprintf("OLD.ptype = 'g2' AND OLD.v0 = '%s'", code))

	err := e.svc.RoleUpdate(ctx, &admindto.RoleUpdateReq{
		ID: roleID, RoleName: "改名为禁用", Status: adminmodel.RoleStatusDisabled,
	})
	wantErr(t, err, "回滚")

	var role adminmodel.RoleEntity
	if err := e.db.First(&role, roleID).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if role.Status != adminmodel.RoleStatusEnabled {
		t.Fatalf("g2 删除失败时角色状态应回滚为启用: got=%d", role.Status)
	}
	if role.RoleName != "故障注入角色" {
		t.Fatalf("g2 删除失败时角色名称也应回滚: got=%q", role.RoleName)
	}

	// g2 仍在 → 角色仍处于启用态（内存与库一致，不存在「库里禁用、内存放行」的错位）
	ok, err := pkgcasbin.GetEnforcer().HasNamedGroupingPolicy("g2", code, "active")
	wantErr(t, err, "")
	if !ok {
		t.Fatalf("回滚后 g2 active 应仍在")
	}
	var g2Rows int64
	if err := e.db.Table("sys_casbin_rule").
		Where("ptype = 'g2' AND v0 = ?", code).Count(&g2Rows).Error; err != nil {
		t.Fatalf("统计 g2 行失败: %v", err)
	}
	if g2Rows != 1 {
		t.Fatalf("回滚后 g2 应仍恰好一行: got=%d", g2Rows)
	}
}

// TestRoleCreateG2InsertFailureRollsBackRoleRow 新建启用角色时 g2 写入失败 → sys_role 不留行。
func TestRoleCreateG2InsertFailureRollsBackRoleRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_txcreate_" + uniq("")
	installPolicyFailTrigger(t, e, "INSERT",
		fmt.Sprintf("NEW.ptype = 'g2' AND NEW.v0 = '%s'", code))

	err := e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{RoleCode: code, RoleName: "半截角色"})
	wantErr(t, err, "回滚")

	var count int64
	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("role_code = ?", code).Count(&count).Error; err != nil {
		t.Fatalf("统计角色失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("g2 写入失败时不应留下角色行: count=%d", count)
	}
}

// TestRoleUpdateActivateExistingG2Idempotent 库里状态与 g2 已经错位（停用但 g2 还在）时，
// 重新启用不报错、不产生重复行 —— 锁定事务版启用写入的幂等（ON CONFLICT DO NOTHING）。
func TestRoleUpdateActivateExistingG2Idempotent(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_idem_" + uniq("")
	roleID := createRole(t, e, code, "错位角色")
	// 绕过 service 直接把库里状态改成禁用，制造「status=0 但 g2 active 仍在」的错位
	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("id = ?", roleID).
		Update("status", adminmodel.RoleStatusDisabled).Error; err != nil {
		t.Fatalf("改写角色状态失败: %v", err)
	}

	err := e.svc.RoleUpdate(ctx, &admindto.RoleUpdateReq{
		ID: roleID, RoleName: "错位角色", Status: adminmodel.RoleStatusEnabled,
	})
	wantErr(t, err, "")

	var g2Rows int64
	if err := e.db.Table("sys_casbin_rule").
		Where("ptype = 'g2' AND v0 = ?", code).Count(&g2Rows).Error; err != nil {
		t.Fatalf("统计 g2 行失败: %v", err)
	}
	if g2Rows != 1 {
		t.Fatalf("重复启用不应产生重复 g2 行: got=%d", g2Rows)
	}
	ok, err := pkgcasbin.GetEnforcer().HasNamedGroupingPolicy("g2", code, "active")
	wantErr(t, err, "")
	if !ok {
		t.Fatalf("重新启用后 g2 active 应存在")
	}
}

// TestDeptUpdateDescendantFailureRollsBackAncestors 移动部门时子孙祖先链更新失败 →
// 自身 parent_id/ancestors 与重命名字段整体回滚。
//
// 复现审计形态：自身已改、子孙还在旧祖先链 → pkg/datarule 的 SELF_AND_CHILDREN 匹配错，
// 数据权限范围错乱且不报错。修复后两处写同事务：失败即整体回滚，树保持原样。
func TestDeptUpdateDescendantFailureRollsBackAncestors(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 结构：A(顶级) → C(子) → D(孙)；B(顶级)
	deptA := deptCreate(t, e, "A", "TA_"+uniq(""))
	deptB := deptCreate(t, e, "B", "TB_"+uniq(""))
	deptC := deptCreate(t, e, "C", "TC_"+uniq(""), deptA)
	deptD := deptCreate(t, e, "D", "TD_"+uniq(""), deptC)

	// 故障注入：只对「子孙行」的 UPDATE 抛异常（自身行的 id 不同，仍可正常写）
	installFailTrigger(t, e, "sys_dept", "UPDATE", fmt.Sprintf("OLD.id = %d", deptC))

	err := e.svc.DeptUpdate(ctx, &admindto.DeptUpdateReq{
		ID: deptA, ParentID: deptB, DeptName: "A改名", DeptCode: deptCode(t, e, deptA), Status: 1,
	})
	wantErr(t, err, "回滚")

	var a, b, c, d adminmodel.DeptEntity
	for _, pair := range []struct {
		id   uint64
		dest *adminmodel.DeptEntity
	}{{deptA, &a}, {deptB, &b}, {deptC, &c}, {deptD, &d}} {
		if err := e.db.First(pair.dest, pair.id).Error; err != nil {
			t.Fatalf("查询部门 %d 失败: %v", pair.id, err)
		}
	}

	// 自身：parent_id / ancestors / 名称全部停在原值（这是「两次 Update 合并为一次」的证据）
	if a.ParentID != 0 || a.Ancestors != "0" || a.DeptName != "A" {
		t.Fatalf("子孙更新失败时自身行应整体回滚: %+v", a)
	}
	// 子孙：祖先链保持原样
	if c.Ancestors != "0,"+uint64Str(deptA) {
		t.Fatalf("子孙 C 的祖先链不应被改: got=%q", c.Ancestors)
	}
	if d.Ancestors != "0,"+uint64Str(deptA)+","+uint64Str(deptC) {
		t.Fatalf("孙部门 D 的祖先链不应被改: got=%q", d.Ancestors)
	}
	// 无关的 B 不受影响
	if b.Ancestors != "0" {
		t.Fatalf("B 与本次移动无关: got=%q", b.Ancestors)
	}
}
