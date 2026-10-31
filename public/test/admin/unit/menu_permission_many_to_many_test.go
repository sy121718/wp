package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func menuPermissionCodes(t *testing.T, e *env, menuID uint64) []string {
	t.Helper()
	var codes []string
	if err := e.db.Table("sys_menu_permission").Where("menu_id = ?", menuID).
		Order("permission_code ASC").Pluck("permission_code", &codes).Error; err != nil {
		t.Fatalf("查询菜单权限关联失败: %v", err)
	}
	return codes
}

func TestMenuHoldsMultiplePermissionCodes(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	codeA := "mpm_a:" + uniq("")
	codeB := "mpm_b:" + uniq("")
	createPerm(t, e, codeA, "/api/mpm/a")
	createPerm(t, e, codeB, "/api/mpm/b")

	title := "多码菜单 " + uniq("")

	wantErr(t, e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: title, Type: adminmodel.MenuTypeMenu, Path: "/mpm/" + uniq(""),
		PermissionCodes: []string{codeA, codeB, codeA}, Status: 1,
	}), "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", title).First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	got := menuPermissionCodes(t, e, menu.ID)
	if len(got) != 2 || got[0] != codeA || got[1] != codeB {
		t.Fatalf("关联行应为去重后的两条 [%s %s]，实际 %v", codeA, codeB, got)
	}

	if menu.PermissionCode != nil {
		t.Fatalf("界面写入不应再改旧列 permission_code，实际为 %q", *menu.PermissionCode)
	}
}

func TestRoleMenuSaveCollectsEveryCodeOfMenu(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	codeA := "mpm_ra:" + uniq("")
	codeB := "mpm_rb:" + uniq("")
	createPerm(t, e, codeA, "/api/mpm/ra")
	createPerm(t, e, codeB, "/api/mpm/rb")

	title := "角色多码菜单 " + uniq("")
	wantErr(t, e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: title, Type: adminmodel.MenuTypeMenu, Path: "/mpm/role/" + uniq(""),
		PermissionCodes: []string{codeA, codeB}, Status: 1,
	}), "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", title).First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	roleCode := "role_mpm_" + uniq("")
	roleID := createRole(t, e, roleCode, "多码角色")

	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menu.ID}}); err != nil {
		t.Fatalf("保存角色菜单失败: %v", err)
	}

	perms, err := pkgcasbin.GetRolePermissions(roleCode)
	if err != nil {
		t.Fatalf("读取角色策略失败: %v", err)
	}
	have := map[string]bool{}
	for _, p := range perms {
		if len(p) > 2 {
			have[p[2]] = true
		}
	}
	if !have[codeA] || !have[codeB] {
		t.Fatalf("勾一个菜单应把它绑的所有码都写进策略，缺 %s/%s（实得 %v）", codeA, codeB, have)
	}
}

func TestPermissionReferenceCountDedupesByMenuAndDeleteClearsLinks(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	codeA := "mpm_ca:" + uniq("")
	codeB := "mpm_cb:" + uniq("")
	createPerm(t, e, codeA, "/api/mpm/ca")
	createPerm(t, e, codeB, "/api/mpm/cb")

	title := "计数菜单 " + uniq("")
	wantErr(t, e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: title, Type: adminmodel.MenuTypeMenu, Path: "/mpm/count/" + uniq(""),
		PermissionCodes: []string{codeA, codeB}, Status: 1,
	}), "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", title).First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	n, err := e.svc.CountByPermissionCodes(ctx, []string{codeA, codeB})
	wantErr(t, err, "")
	if n != 1 {
		t.Fatalf("引用计数应按菜单去重，期望 1，实际 %d", n)
	}

	wantErr(t, e.svc.MenuDelete(ctx, &admindto.MenuDeleteReq{IDs: []uint64{menu.ID}}), "")
	if left := menuPermissionCodes(t, e, menu.ID); len(left) != 0 {
		t.Fatalf("软删菜单后关联行应清空，实际 %v", left)
	}
	n, err = e.svc.CountByPermissionCodes(ctx, []string{codeA})
	wantErr(t, err, "")
	if n != 0 {
		t.Fatalf("软删后引用计数应为 0，实际 %d", n)
	}
}
