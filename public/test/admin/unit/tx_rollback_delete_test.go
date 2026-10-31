package unit

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func countPolicyRows(t *testing.T, e *env, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := e.db.Table("sys_casbin_rule").Where(where, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计策略行失败: %v", err)
	}
	return n
}

func TestRoleDeletePolicyFailureRollsBackRoleRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_delfail_" + uniq("")
	roleID := createRole(t, e, code, "待删角色")

	if err := pkgcasbin.ReplaceRolePermissions(code, [][3]string{{"/api/tx_del", "GET", "perm_tx_del"}}); err != nil {
		t.Fatalf("写入角色权限失败: %v", err)
	}
	if err := pkgcasbin.ReplaceRoleUsers(code, []string{"10001"}); err != nil {
		t.Fatalf("写入角色用户绑定失败: %v", err)
	}

	installPolicyFailTrigger(t, e, "DELETE", fmt.Sprintf("OLD.ptype = 'p' AND OLD.v0 = '%s'", code))

	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: roleID})
	wantErr(t, err, "回滚")

	var role adminmodel.RoleEntity
	if err := e.db.First(&role, roleID).Error; err != nil {
		t.Fatalf("策略写失败时角色行不应该被删掉: %v", err)
	}

	if n := countPolicyRows(t, e, "ptype = 'p' AND v0 = ?", code); n != 1 {
		t.Fatalf("回滚后角色应仍持有 1 行 p 策略: got=%d", n)
	}
	if n := countPolicyRows(t, e, "ptype = 'g' AND v1 = ?", code); n != 1 {
		t.Fatalf("回滚后角色应仍有 1 行 g 绑定: got=%d", n)
	}
	if n := countPolicyRows(t, e, "ptype = 'g2' AND v0 = ?", code); n != 1 {
		t.Fatalf("回滚后角色应仍有 1 行 g2 启用标记: got=%d", n)
	}
}

func TestRoleDeleteEntityFailureRollsBackPolicies(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_delfail2_" + uniq("")
	roleID := createRole(t, e, code, "待删角色2")
	if err := pkgcasbin.ReplaceRolePermissions(code, [][3]string{{"/api/tx_del2", "GET", "perm_tx_del2"}}); err != nil {
		t.Fatalf("写入角色权限失败: %v", err)
	}
	if err := pkgcasbin.ReplaceRoleUsers(code, []string{"10002"}); err != nil {
		t.Fatalf("写入角色用户绑定失败: %v", err)
	}

	installFailTrigger(t, e, "sys_role", "DELETE", fmt.Sprintf("OLD.id = %d", roleID))

	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: roleID})
	if err == nil {
		t.Fatalf("实体删除失败必须把错误回给调用方（不允许静默吞掉）")
	}

	var role adminmodel.RoleEntity
	if err := e.db.First(&role, roleID).Error; err != nil {
		t.Fatalf("实体删除失败时角色行应保持原样: %v", err)
	}
	if n := countPolicyRows(t, e, "ptype = 'p' AND v0 = ?", code); n != 1 {
		t.Fatalf("实体删除失败时策略行应随事务回滚（旧实现的半截状态就在这）: got=%d", n)
	}
	if n := countPolicyRows(t, e, "ptype = 'g' AND v1 = ?", code); n != 1 {
		t.Fatalf("实体删除失败时 g 绑定应回滚: got=%d", n)
	}
	if n := countPolicyRows(t, e, "ptype = 'g2' AND v0 = ?", code); n != 1 {
		t.Fatalf("实体删除失败时 g2 标记应回滚: got=%d", n)
	}
}

func TestRoleDeleteCommitsRowAndPolicies(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_delok_" + uniq("")
	roleID := createRole(t, e, code, "正常删除")
	if err := pkgcasbin.ReplaceRolePermissions(code, [][3]string{{"/api/tx_delok", "GET", "perm_tx_delok"}}); err != nil {
		t.Fatalf("写入角色权限失败: %v", err)
	}
	if err := pkgcasbin.ReplaceRoleUsers(code, []string{"10003"}); err != nil {
		t.Fatalf("写入角色用户绑定失败: %v", err)
	}
	if _, hit := pkgcasbin.GetCodeByURL("/api/tx_delok", "GET"); !hit {
		t.Fatalf("前置条件失败：删除前 URL→code 映射里应有该权限")
	}

	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: roleID})
	wantErr(t, err, "")

	var count int64
	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("id = ?", roleID).Count(&count).Error; err != nil {
		t.Fatalf("统计角色失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("角色行应已删除: count=%d", count)
	}
	if n := countPolicyRows(t, e, "v0 = ? OR v1 = ?", code, code); n != 0 {
		t.Fatalf("角色删除后不应残留任何策略行: got=%d", n)
	}

	if _, hit := pkgcasbin.GetCodeByURL("/api/tx_delok", "GET"); hit {
		t.Fatalf("角色已删除，内存 URL→code 映射应已刷新")
	}
}

func seedAdminPolicies(t *testing.T, e *env, userID uint64) string {
	t.Helper()
	raw := strconv.FormatUint(userID, 10)
	if err := pkgcasbin.ReplaceUserPermissions(raw, [][3]string{{"/api/user_tx", "GET", "perm_user_tx"}}); err != nil {
		t.Fatalf("写入用户直接权限失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserRoleBindings(raw, []string{"tx_bind_role"}); err != nil {
		t.Fatalf("写入用户角色绑定失败: %v", err)
	}
	return raw
}

func TestAdminDeleteEntityFailureRollsBackPolicies(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	adminID := createAdminDB(t, e.db, "tx_del_admin_"+uniq(""), "tx_del_admin_"+uniq("")+"@example.com")
	raw := seedAdminPolicies(t, e, adminID)

	installFailTrigger(t, e, "sys_admin", "DELETE", fmt.Sprintf("OLD.id = %d", adminID))

	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{Id: []uint64{adminID}, OperatorID: 999999})
	if err == nil {
		t.Fatalf("实体删除失败必须把错误回给调用方（不允许静默吞掉）")
	}

	var count int64
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", adminID).Count(&count).Error; err != nil {
		t.Fatalf("统计管理员失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("实体删除失败时管理员行应保持原样: count=%d", count)
	}
	if n := countPolicyRows(t, e, "v0 = ?", raw); n != 2 {
		t.Fatalf("实体删除失败时策略行应一行不少（p + g）: got=%d", n)
	}
}

func TestAdminDeletePolicyFailureRollsBackAdminRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	adminID := createAdminDB(t, e.db, "tx_del_admin2_"+uniq(""), "tx_del_admin2_"+uniq("")+"@example.com")
	raw := seedAdminPolicies(t, e, adminID)

	installPolicyFailTrigger(t, e, "DELETE", fmt.Sprintf("OLD.ptype = 'p' AND OLD.v0 = '%s'", raw))

	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{Id: []uint64{adminID}, OperatorID: 999999})
	wantErr(t, err, "回滚")

	var count int64
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", adminID).Count(&count).Error; err != nil {
		t.Fatalf("统计管理员失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("策略清失败时管理员行应随事务回滚: count=%d", count)
	}
	if n := countPolicyRows(t, e, "v0 = ?", raw); n != 2 {
		t.Fatalf("回滚后该账号的 p / g 策略行应一行不少: got=%d", n)
	}
}

func TestAdminDeleteCommitsRowAndPolicies(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	adminID := createAdminDB(t, e.db, "tx_del_admin3_"+uniq(""), "tx_del_admin3_"+uniq("")+"@example.com")
	raw := seedAdminPolicies(t, e, adminID)

	res, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{Id: []uint64{adminID}, OperatorID: 999999})
	wantErr(t, err, "")
	if res == nil || res.DeletedCount != 1 {
		t.Fatalf("删除结果应报告 1 条: %+v", res)
	}

	var count int64
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", adminID).Count(&count).Error; err != nil {
		t.Fatalf("统计管理员失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("管理员行应已删除: count=%d", count)
	}
	if n := countPolicyRows(t, e, "v0 = ?", raw); n != 0 {
		t.Fatalf("管理员删除后不应残留策略行: got=%d", n)
	}
	if codes, err := pkgcasbin.GetRoleCodesByUserID(raw); err != nil || len(codes) != 0 {
		t.Fatalf("内存副本里该用户的角色绑定应已清空: codes=%v err=%v", codes, err)
	}
}
