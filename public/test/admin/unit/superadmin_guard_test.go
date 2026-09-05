package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	adminenums "go_wp/internal/module/admin/enums"
	pkgcasbin "go_wp/pkg/casbin"
)

// makeSuperAdmin 把指定管理员置为超管（is_admin=1）。
// 走原始 SQL 绕过 AdminEntity.BeforeUpdate 的 IsAdmin 保护 hook（测试环境直改）。
func makeSuperAdmin(t *testing.T, e *env, id uint64) {
	t.Helper()
	if err := e.db.Exec("UPDATE sys_admin SET is_admin = 1 WHERE id = ?", id).Error; err != nil {
		t.Fatalf("置超管失败: %v", err)
	}
}

// grantAllPermissions 把当前全部启用权限点授权给角色，使其成为「超管角色」
// （权限集覆盖 sys_permission 全部启用权限点，与 roleHasSuperAdminPermission 判定对齐）。
func grantAllPermissions(t *testing.T, e *env, roleCode string) {
	t.Helper()
	var perms []adminmodel.PermissionEntity
	if err := e.db.Where("status = ?", adminmodel.PermissionStatusEnabled).Find(&perms).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if len(perms) == 0 {
		t.Fatalf("前置条件失败：需要至少一个启用权限点")
	}
	policies := make([][3]string, 0, len(perms))
	for _, p := range perms {
		policies = append(policies, [3]string{p.APIPath, p.APIMethod, p.PermissionCode})
	}
	if err := pkgcasbin.ReplaceRolePermissions(roleCode, policies); err != nil {
		t.Fatalf("角色授权失败: %v", err)
	}
}

// TestAdminRoleSaveSuperAdminProtection 用户角色绑定超管保护：
// 目标用户是超管账号，或目标角色含超管权限时，仅超管可操作。
func TestAdminRoleSaveSuperAdminProtection(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 前置：两个权限点（构成「全部启用权限点」全集）
	codeA := "sa_" + uniq("a")
	codeB := "sa_" + uniq("b")
	createPerm(t, e, codeA, "/api/sa-a")
	createPerm(t, e, codeB, "/api/sa-b")

	operator := createAdminDB(t, e.db, uniq("op"), uniq("op")+"@example.com") // 普通操作者
	target := createAdminDB(t, e.db, uniq("tgt"), uniq("tgt")+"@example.com")
	roleCode := "sa_" + uniq("role")
	createRole(t, e, roleCode, "测试角色")

	// 场景 A：普通操作者修改超管账号的角色绑定 → 拒绝
	makeSuperAdmin(t, e, target)
	_, err := e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: target, RoleCodes: []string{roleCode}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	// 场景 B：普通操作者把用户绑到超管角色（权限覆盖全部权限点）→ 拒绝
	makeSuperAdmin(t, e, target) // 保持不变（超管）
	grantAllPermissions(t, e, roleCode)
	_, err = e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: operator, RoleCodes: []string{roleCode}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	// 场景 C：超管操作者操作超管目标 → 放行
	superOperator := createAdminDB(t, e.db, uniq("sop"), uniq("sop")+"@example.com")
	makeSuperAdmin(t, e, superOperator)
	_, err = e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: target, RoleCodes: []string{roleCode}, OperatorID: superOperator,
	})
	wantErr(t, err, "")

	// 场景 D：普通操作者操作普通目标 + 普通角色 → 放行（回归）
	plainRole := "sa_" + uniq("plain")
	createRole(t, e, plainRole, "普通角色")
	plainTarget := createAdminDB(t, e.db, uniq("pt"), uniq("pt")+"@example.com")
	_, err = e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: plainTarget, RoleCodes: []string{plainRole}, OperatorID: operator,
	})
	wantErr(t, err, "")
}

// TestRoleUserSaveSuperAdminProtection 角色用户绑定超管保护：
// 目标角色含超管权限，或目标用户列表含超管账号时，仅超管可操作。
func TestRoleUserSaveSuperAdminProtection(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	createPerm(t, e, "su_"+uniq("a"), "/api/su-a")
	createPerm(t, e, "su_"+uniq("b"), "/api/su-b")

	operator := createAdminDB(t, e.db, uniq("op"), uniq("op")+"@example.com") // 普通操作者
	u1 := createAdminDB(t, e.db, uniq("u1"), uniq("u1")+"@example.com")
	u2 := createAdminDB(t, e.db, uniq("u2"), uniq("u2")+"@example.com")

	// 场景 A：普通操作者把用户绑进超管角色 → 拒绝
	superRole := "su_" + uniq("role")
	superRoleID := createRole(t, e, superRole, "超管角色")
	grantAllPermissions(t, e, superRole)
	_, err := e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: superRoleID, UserIDs: []uint64{u1}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	// 场景 B：普通操作者把超管账号绑进普通角色 → 拒绝（目标用户含超管）
	plainRole := "su_" + uniq("plain")
	plainRoleID := createRole(t, e, plainRole, "普通角色")
	superUser := createAdminDB(t, e.db, uniq("su"), uniq("su")+"@example.com")
	makeSuperAdmin(t, e, superUser)
	_, err = e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: plainRoleID, UserIDs: []uint64{superUser}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	// 场景 C：超管操作者往超管角色绑普通用户 → 放行
	superOperator := createAdminDB(t, e.db, uniq("sop"), uniq("sop")+"@example.com")
	makeSuperAdmin(t, e, superOperator)
	_, err = e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: superRoleID, UserIDs: []uint64{u1, u2}, OperatorID: superOperator,
	})
	wantErr(t, err, "")

	// 场景 D：普通操作者 + 普通角色 + 普通用户 → 放行（回归）
	_, err = e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: plainRoleID, UserIDs: []uint64{u1}, OperatorID: operator,
	})
	wantErr(t, err, "")
}
