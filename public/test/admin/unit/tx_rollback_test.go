package unit

import (
	"context"
	"fmt"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func installPolicyFailTrigger(t *testing.T, e *env, event, whenExpr string) {
	t.Helper()
	installFailTrigger(t, e, "sys_casbin_rule", event, whenExpr)
}

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

func mustPermRow(t *testing.T, e *env, id uint64) adminmodel.PermissionEntity {
	t.Helper()
	var perm adminmodel.PermissionEntity
	if err := e.db.First(&perm, id).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	return perm
}

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

	installPolicyFailTrigger(t, e, "INSERT",
		fmt.Sprintf("NEW.ptype = 'p' AND NEW.v3 = '%s'", code))

	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "改名后", Module: "console",
		APIPath: "/api/tx_new", APIMethod: "POST", Status: 1,
	})
	wantErr(t, err, "回滚")

	after := mustPermRow(t, e, permID)
	if after.PermissionName != before.PermissionName || after.Module != before.Module ||
		after.APIPath != before.APIPath || after.APIMethod != before.APIMethod ||
		after.Status != before.Status {
		t.Fatalf("策略写失败时权限点行应整体回滚：before=%+v after=%+v", before, after)
	}
	if after.APIPath != "/api/tx_old" || after.APIMethod != "GET" {
		t.Fatalf("权限点定义应保持旧值: %+v", after)
	}

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

	if _, hit := pkgcasbin.GetCodeByURL("/api/ok_new", "POST"); !hit {
		t.Fatalf("提交后内存 URL→code 映射未刷新")
	}
}

func TestRoleUpdateG2DeleteFailureRollsBackStatus(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_txfail_" + uniq("")
	roleID := createRole(t, e, code, "故障注入角色")

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

func TestRoleUpdateActivateExistingG2Idempotent(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_idem_" + uniq("")
	roleID := createRole(t, e, code, "错位角色")

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

func TestDeptUpdateDescendantFailureRollsBackAncestors(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	deptA := deptCreate(t, e, "A", "TA_"+uniq(""))
	deptB := deptCreate(t, e, "B", "TB_"+uniq(""))
	deptC := deptCreate(t, e, "C", "TC_"+uniq(""), deptA)
	deptD := deptCreate(t, e, "D", "TD_"+uniq(""), deptC)

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

	if a.ParentID != 0 || a.Ancestors != "0" || a.DeptName != "A" {
		t.Fatalf("子孙更新失败时自身行应整体回滚: %+v", a)
	}

	if c.Ancestors != "0,"+uint64Str(deptA) {
		t.Fatalf("子孙 C 的祖先链不应被改: got=%q", c.Ancestors)
	}
	if d.Ancestors != "0,"+uint64Str(deptA)+","+uint64Str(deptC) {
		t.Fatalf("孙部门 D 的祖先链不应被改: got=%q", d.Ancestors)
	}

	if b.Ancestors != "0" {
		t.Fatalf("B 与本次移动无关: got=%q", b.Ancestors)
	}
}
