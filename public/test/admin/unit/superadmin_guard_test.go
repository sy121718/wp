package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func makeSuperAdmin(t *testing.T, e *env, id uint64) {
	t.Helper()
	if err := e.db.Exec("UPDATE sys_admin SET is_admin = 1 WHERE id = ?", id).Error; err != nil {
		t.Fatalf("置超管失败: %v", err)
	}
}

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

func TestAdminRoleSaveSuperAdminProtection(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	codeA := "sa_" + uniq("a")
	codeB := "sa_" + uniq("b")
	createPerm(t, e, codeA, "/api/sa-a")
	createPerm(t, e, codeB, "/api/sa-b")

	operator := createAdminDB(t, e.db, uniq("op"), uniq("op")+"@example.com")
	target := createAdminDB(t, e.db, uniq("tgt"), uniq("tgt")+"@example.com")
	roleCode := "sa_" + uniq("role")
	createRole(t, e, roleCode, "测试角色")

	makeSuperAdmin(t, e, target)
	_, err := e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: target, RoleCodes: []string{roleCode}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	makeSuperAdmin(t, e, target)
	grantAllPermissions(t, e, roleCode)
	_, err = e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: operator, RoleCodes: []string{roleCode}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	superOperator := createAdminDB(t, e.db, uniq("sop"), uniq("sop")+"@example.com")
	makeSuperAdmin(t, e, superOperator)
	_, err = e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: target, RoleCodes: []string{roleCode}, OperatorID: superOperator,
	})
	wantErr(t, err, "")

	plainRole := "sa_" + uniq("plain")
	createRole(t, e, plainRole, "普通角色")
	plainTarget := createAdminDB(t, e.db, uniq("pt"), uniq("pt")+"@example.com")
	_, err = e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{
		UserID: plainTarget, RoleCodes: []string{plainRole}, OperatorID: operator,
	})
	wantErr(t, err, "")
}

func TestRoleUserSaveSuperAdminProtection(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	createPerm(t, e, "su_"+uniq("a"), "/api/su-a")
	createPerm(t, e, "su_"+uniq("b"), "/api/su-b")

	operator := createAdminDB(t, e.db, uniq("op"), uniq("op")+"@example.com")
	u1 := createAdminDB(t, e.db, uniq("u1"), uniq("u1")+"@example.com")
	u2 := createAdminDB(t, e.db, uniq("u2"), uniq("u2")+"@example.com")

	superRole := "su_" + uniq("role")
	superRoleID := createRole(t, e, superRole, "超管角色")
	grantAllPermissions(t, e, superRole)
	_, err := e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: superRoleID, UserIDs: []uint64{u1}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	plainRole := "su_" + uniq("plain")
	plainRoleID := createRole(t, e, plainRole, "普通角色")
	superUser := createAdminDB(t, e.db, uniq("su"), uniq("su")+"@example.com")
	makeSuperAdmin(t, e, superUser)
	_, err = e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: plainRoleID, UserIDs: []uint64{superUser}, OperatorID: operator,
	})
	wantErr(t, err, adminenums.ErrSuperAdminOnly)

	superOperator := createAdminDB(t, e.db, uniq("sop"), uniq("sop")+"@example.com")
	makeSuperAdmin(t, e, superOperator)
	_, err = e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: superRoleID, UserIDs: []uint64{u1, u2}, OperatorID: superOperator,
	})
	wantErr(t, err, "")

	_, err = e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: plainRoleID, UserIDs: []uint64{u1}, OperatorID: operator,
	})
	wantErr(t, err, "")
}
