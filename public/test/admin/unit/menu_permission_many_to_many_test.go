package unit

// menu_permission_many_to_many_test.go — 菜单 ↔ 权限点多对多（迁移 470）的链路用例。
//
// 迁移 470 把「一个菜单最多挂一个权限码」升级成关联表 sys_menu_permission。
// 这一组用例钉住只有真实库才能验证的三件事：
//
//	① 一个菜单挂多个码：全部进关联表（重复提交去重），而**旧列 permission_code 仍为空** ——
//	   界面不再写它，它只服务 seed 的写入与幂等判据（12 条 seed 拿它定位菜单行）；
//	② 角色分权按「勾菜单」收集码：勾一个菜单把它的**每个**码都写进 Casbin p 策略 ——
//	   这是多对多真正的收益（此前一个菜单节点只能换出一个权限点）；
//	③ 权限点删除前的引用计数**按菜单去重**：同一菜单挂两个待删码只算一个菜单，
//	   否则「删 2 个权限点会波及 3 个菜单」这种数字会凭空出现，操作者无法核对。

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

// menuPermissionCodes 直接读关联表（绕开应用层）：断言的是「库里真有什么」。
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
	// 第三个码与第一个重复：去重是写入侧的职责（唯一索引只是兜底），
	// 表单里多点一下不该变成一次写入失败。
	wantErr(t, e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: title, Type: adminmodel.MenuTypeMenu, Path: "/mpm/" + uniq(""), Component: "view.mpm",
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
	// 旧列必须保持为空：界面一旦写它，12 条以 permission_code 为判据的 seed 就会漂移。
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
		Title: title, Type: adminmodel.MenuTypeMenu, Path: "/mpm/role/" + uniq(""), Component: "view.mpmRole",
		PermissionCodes: []string{codeA, codeB}, Status: 1,
	}), "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", title).First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	roleCode := "role_mpm_" + uniq("")
	roleID := createRole(t, e, roleCode, "多码角色")

	// 只勾这一个菜单节点：它的两个码都该进 p 策略。
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
		Title: title, Type: adminmodel.MenuTypeMenu, Path: "/mpm/count/" + uniq(""), Component: "view.mpmCount",
		PermissionCodes: []string{codeA, codeB}, Status: 1,
	}), "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", title).First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	// 两个待删码挂在**同一个**菜单上 → 引用计数是 1 个菜单，不是 2。
	n, err := e.svc.CountByPermissionCodes(ctx, []string{codeA, codeB})
	wantErr(t, err, "")
	if n != 1 {
		t.Fatalf("引用计数应按菜单去重，期望 1，实际 %d", n)
	}

	// 软删菜单 → 关联行必须一起清掉：留下的关联行会让权限点的引用计数永久虚高
	// （于是那个权限点再也删不掉），并让授权回显冒出一个已删菜单的勾选项。
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
