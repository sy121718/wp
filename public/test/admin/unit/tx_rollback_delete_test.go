// tx_rollback_delete_test.go — RoleDelete / AdminDelete 的事务回滚证明
// （2026-09-19 admin 收口批，接在 tx_rollback_test.go 的 PermUpdate / RoleUpdate / RoleCreate /
// DeptUpdate 之后：同样是「两处持久化写各自提交」的形态）。
//
// 为什么用数据库触发器注入故障而不是给生产代码开测试开关：见 tx_rollback_test.go 文件头的说明
// （走完全真实的 service → model → gorm → PG 路径，不在生产代码上留后门）。
//
// 两处写的**先后顺序不同**，所以两侧都要钉：
//
//	· RoleDelete：先删策略行、再删 sys_role —— 「实体删除失败」这一侧才会真正验证回滚
//	  （策略行已经删掉了，必须随事务一起回来）；
//	· AdminDelete：先删 sys_admin 行、再删策略行 —— 「策略写失败」这一侧才是强断言。
//
// 两侧都断言，是为了无论将来谁调整顺序，都有一条用例真的在验证回滚而不是同义反复。
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

// countPolicyRows 统计某条件下 sys_casbin_rule 的行数。
func countPolicyRows(t *testing.T, e *env, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := e.db.Table("sys_casbin_rule").Where(where, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计策略行失败: %v", err)
	}
	return n
}

// --- RoleDelete ---

// TestRoleDeletePolicyFailureRollsBackRoleRow 策略行删除失败 → sys_role 行不删。
//
// 旧实现：DeleteRoleAllPolicies 失败时 sys_role 还没删，看起来「没事」；真正危险的是另一半 ——
// 策略删成功、实体删除失败（见下一条）。这一条先钉住「策略写失败必须整体回滚」。
func TestRoleDeletePolicyFailureRollsBackRoleRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_delfail_" + uniq("")
	roleID := createRole(t, e, code, "待删角色")
	// 该角色持有权限 + 用户绑定 + g2 启用标记，三类策略行都真实存在
	if err := pkgcasbin.ReplaceRolePermissions(code, [][3]string{{"/api/tx_del", "GET", "perm_tx_del"}}); err != nil {
		t.Fatalf("写入角色权限失败: %v", err)
	}
	if err := pkgcasbin.ReplaceRoleUsers(code, []string{"10001"}); err != nil {
		t.Fatalf("写入角色用户绑定失败: %v", err)
	}

	// 故障注入：删该角色的 p 行时抛异常
	installPolicyFailTrigger(t, e, "DELETE", fmt.Sprintf("OLD.ptype = 'p' AND OLD.v0 = '%s'", code))

	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: roleID})
	wantErr(t, err, "回滚") // 错误必须回给调用方，不允许静默吞掉

	// 1) 角色行必须还在
	var role adminmodel.RoleEntity
	if err := e.db.First(&role, roleID).Error; err != nil {
		t.Fatalf("策略写失败时角色行不应该被删掉: %v", err)
	}
	// 2) 策略行也必须原封不动（p / g / g2 三类）
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

// TestRoleDeleteEntityFailureRollsBackPolicies 实体行删除失败 → 策略行不删。
//
// 这是 RoleDelete 最危险的半截状态：策略行**已经先删掉了**（事务内），随后删 sys_role 失败；
// 修复前策略删除是独立提交的，于是库里留下「角色还在、权限一个不剩」——
// 该角色下所有人瞬间失权，而报错只回给这一次请求，列表页上完全看不出异常。
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

	// 故障注入：删 sys_role 该行时抛异常（策略行的删除已经发生在这个事务里）
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

// TestRoleDeleteCommitsRowAndPolicies 正向对照：删除成功时角色行与三类策略行一起消失，
// 且**内存副本**（Enforce 读的那一份）也已刷新 —— 库里删了、内存还放行是最坏的一种不一致。
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
	// 内存副本：删除后该 URL→code 映射必须失效
	if _, hit := pkgcasbin.GetCodeByURL("/api/tx_delok", "GET"); hit {
		t.Fatalf("角色已删除，内存 URL→code 映射应已刷新")
	}
}

// --- AdminDelete ---

// seedAdminPolicies 给一个管理员挂上直接权限（p）与角色绑定（g），返回其 id 字符串。
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

// TestAdminDeleteEntityFailureRollsBackPolicies sys_admin 删除失败 → 策略行不删。
//
// AdminDelete 的顺序是「先删实体、再删策略」，所以这一侧其实是在断言「前一步失败时
// 后一步根本没机会发生」；真正的强断言在下面那条（策略写失败 → 已删的实体行要回来）。
// 两条都留着：顺序一旦被改动，至少有一条还在真的验证回滚。
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

// TestAdminDeletePolicyFailureRollsBackAdminRow 策略行删除失败 → sys_admin 行不删。
//
// 复现审计形态：实体行已经先删掉了（事务内），随后清策略失败；修复前实体删除是独立提交的，
// 于是「管理员没了、策略还在」—— 同一 id 被复用时会静默继承旧权限；反向的半截状态
// （策略先删、实体没删）则是账号还在却已失权。现在整体回滚：账号与策略都保持原样。
func TestAdminDeletePolicyFailureRollsBackAdminRow(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	adminID := createAdminDB(t, e.db, "tx_del_admin2_"+uniq(""), "tx_del_admin2_"+uniq("")+"@example.com")
	raw := seedAdminPolicies(t, e, adminID)

	// 故障注入：删该账号的 p 行时抛异常
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

// TestAdminDeleteCommitsRowAndPolicies 正向对照：删除成功时实体行与策略行一起消失，
// 且内存副本已刷新。
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
