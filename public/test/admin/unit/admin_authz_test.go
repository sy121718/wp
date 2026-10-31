package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func TestEffectivePermissionCodesMergesRoleAndDirect(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	menuCode := "eff_menu_" + uniq("")
	btnCode := "eff_btn_" + uniq("")
	createPerm(t, e, menuCode, "/api/eff_menu")
	createPerm(t, e, btnCode, "/api/eff_btn")

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "有效码菜单", Type: adminmodel.MenuTypeMenu, Path: "/eff/a",
		PermissionCodes: []string{menuCode}, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "有效码菜单").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	roleCode := "eff_role_" + uniq("")
	roleID := createRole(t, e, roleCode, "有效码角色")
	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menu.ID}}); err != nil {
		t.Fatalf("角色授权失败: %v", err)
	}
	userID := uint64(5252)
	if err := pkgcasbin.ReplaceUserRoleBindings(idStr(userID), []string{roleCode}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}

	if err := pkgcasbin.ReplaceUserPermissions(idStr(userID), [][3]string{{"/api/eff_btn", "GET", btnCode}}); err != nil {
		t.Fatalf("写入用户直接权限失败: %v", err)
	}

	codes, err := e.svc.EffectivePermissionCodes(ctx, userID)
	wantErr(t, err, "")
	got := map[string]bool{}
	for _, c := range codes {
		got[c] = true
	}
	if !got[menuCode] {
		t.Fatalf("角色继承的码应出现在有效权限集里: %v", codes)
	}
	if !got[btnCode] {
		t.Fatalf("用户直接授权的码应出现在有效权限集里: %v", codes)
	}

	if err := pkgcasbin.ReplaceUserPermissions(idStr(userID), [][3]string{
		{"/api/eff_btn", "GET", btnCode}, {"/api/eff_menu", "GET", menuCode},
	}); err != nil {
		t.Fatalf("写入重复直接权限失败: %v", err)
	}
	codes2, err := e.svc.EffectivePermissionCodes(ctx, userID)
	wantErr(t, err, "")
	seen := 0
	for _, c := range codes2 {
		if c == menuCode {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("角色与直接权限重叠时应去重，实际出现 %d 次: %v", seen, codes2)
	}
}

func TestAdminMenuListDirectAndEffective(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	codeA := "am_a_" + uniq("")
	codeB := "am_b_" + uniq("")
	createPerm(t, e, codeA, "/api/am_a")
	createPerm(t, e, codeB, "/api/am_b")

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "AM菜单A", Type: adminmodel.MenuTypeMenu, Path: "/am/a",
		PermissionCodes: []string{codeA}, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menuA adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "AM菜单A").First(&menuA).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "AM菜单B", Type: adminmodel.MenuTypeMenu, Path: "/am/b",
		PermissionCodes: []string{codeB}, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menuB adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "AM菜单B").First(&menuB).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	userID := uint64(777)

	roleID := createRole(t, e, "am_role_"+uniq(""), "AM角色")
	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menuA.ID}}); err != nil {
		t.Fatalf("角色授权失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserRoleBindings(idStr(userID), []string{codeOfRole(t, e, roleID)}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}

	if err := pkgcasbin.ReplaceUserPermissions(idStr(userID), [][3]string{{"/api/am_b", "GET", codeB}}); err != nil {
		t.Fatalf("写入直接权限失败: %v", err)
	}

	res, err := e.svc.AdminMenuList(ctx, &admindto.AdminMenuListReq{UserID: userID})
	wantErr(t, err, "")
	if len(res.DirectMenuIDs) != 1 || res.DirectMenuIDs[0] != menuB.ID {
		t.Fatalf("直接菜单不符: %v", res.DirectMenuIDs)
	}
	if len(res.EffectiveMenuIDs) != 2 {
		t.Fatalf("有效菜单应含 A+B: %v", res.EffectiveMenuIDs)
	}
}

func TestAdminRoleSaveAndList(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "ar_" + uniq("")
	createRole(t, e, code, "用户角色")
	userID := uint64(31337)

	_, err := e.svc.AdminRoleSave(ctx, &admindto.AdminRoleSaveReq{UserID: userID, RoleCodes: []string{code}})
	wantErr(t, err, "")

	list, err := e.svc.AdminRoleList(ctx, &admindto.AdminRoleListReq{UserID: userID})
	wantErr(t, err, "")
	if len(list.List) != 1 || list.List[0].RoleCode != code {
		t.Fatalf("角色列表不符: %+v", list.List)
	}
}

func TestBuildAuthorizedTree(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "tree_" + uniq("")
	createPerm(t, e, code, "/api/tree2")

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "授权目录", Type: adminmodel.MenuTypeDirectory, Path: "/authz", Status: 1,
	}); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	var dir adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "授权目录").First(&dir).Error; err != nil {
		t.Fatalf("查询目录失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "授权菜单", Type: adminmodel.MenuTypeMenu, Path: "/authz/m",
		PermissionCodes: []string{code}, ParentID: dir.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}

	otherCode := "tree_noauth_" + uniq("")
	createPerm(t, e, otherCode, "/api/noauth")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "未授权菜单", Type: adminmodel.MenuTypeMenu, Path: "/noauth",
		PermissionCodes: []string{otherCode}, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}

	tree, err := e.svc.BuildAuthorizedTree(ctx, []string{code})
	wantErr(t, err, "")

	root := findMenuNodeByTitle(tree, "授权目录")
	if root == nil {
		t.Fatalf("授权目录应出现在授权树里: %+v", tree)
	}
	if len(root.Children) != 1 || root.Children[0].Title != "授权菜单" {
		t.Fatalf("授权树结构不符: %+v", root)
	}
	if hasMenuNodeTitle(tree, "未授权菜单") {
		t.Fatalf("未授权菜单不应出现在授权树里: %+v", tree)
	}
}

func findMenuNodeByTitle(nodes []admindto.MenuTreeNode, title string) *admindto.MenuTreeNode {
	for i := range nodes {
		if nodes[i].Title == title {
			return &nodes[i]
		}
	}
	return nil
}

func hasMenuNodeTitle(nodes []admindto.MenuTreeNode, title string) bool {
	for _, n := range nodes {
		if n.Title == title || hasMenuNodeTitle(n.Children, title) {
			return true
		}
	}
	return false
}
