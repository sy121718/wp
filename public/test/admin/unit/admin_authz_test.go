package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

// TestAdminRoutesBuild 用户动态路由：角色继承 + 直接权限合并，目录自动补齐。
func TestAdminRoutesBuild(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	// 数据：目录(1) → 菜单A(code_a)；按钮(code_btn) 挂在菜单A下
	menuCode := "routes_menu_" + uniq("")
	btnCode := "routes_btn_" + uniq("")
	createPerm(t, e, menuCode, "/api/routes")
	createPerm(t, e, btnCode, "/api/btn")

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "路由目录", Type: adminmodel.MenuTypeDirectory, Path: "/routes", Status: 1, SortOrder: 1,
	}); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	var dir adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "路由目录").First(&dir).Error; err != nil {
		t.Fatalf("查询目录失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "菜单A", Type: adminmodel.MenuTypeMenu, Path: "/routes/a",
		Component: "view.routesA", PermissionCode: menuCode, ParentID: dir.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menuA adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "菜单A").First(&menuA).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "按钮X", Type: adminmodel.MenuTypeButton, Path: "",
		PermissionCode: btnCode, ParentID: menuA.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建按钮失败: %v", err)
	}

	// 角色绑定菜单A权限 + 用户绑定角色
	roleCode := "routes_role_" + uniq("")
	roleID := createRole(t, e, roleCode, "路由角色")
	_, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menuA.ID}})
	wantErr(t, err, "")
	userID := uint64(4242)
	if err := pkgcasbin.ReplaceUserRoleBindings(idStr(userID), []string{roleCode}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}
	// 用户直接权限：按钮
	if err := pkgcasbin.ReplaceUserPermissions(idStr(userID), [][3]string{{"/api/btn", "GET", btnCode}}); err != nil {
		t.Fatalf("写入用户直接权限失败: %v", err)
	}

	res, err := e.svc.AdminRoutes(ctx, userID, "zh-CN")
	wantErr(t, err, "")
	if len(res.Routes) != 1 {
		t.Fatalf("路由树应含 1 个目录: %d", len(res.Routes))
	}
	root := res.Routes[0]
	if root.Meta.Title != "路由目录" {
		t.Fatalf("目录标题不符: %s", root.Meta.Title)
	}
	if len(root.Children) != 1 {
		t.Fatalf("目录下应有菜单A: %d", len(root.Children))
	}
	menuNode := root.Children[0]
	if menuNode.Component != "view.routesA" {
		t.Fatalf("菜单组件不符: %s", menuNode.Component)
	}
	// 按钮权限应进入菜单节点的 auths
	foundBtn := false
	for _, auth := range menuNode.Meta.Auths {
		if auth == btnCode {
			foundBtn = true
		}
	}
	if !foundBtn {
		t.Fatalf("按钮权限应出现在菜单 auths: %v", menuNode.Meta.Auths)
	}

	hasMenu := false
	for _, code := range res.PermissionCodes {
		if code == menuCode {
			hasMenu = true
		}
	}
	if !hasMenu {
		t.Fatalf("PermissionCodes 应含菜单权限: %v", res.PermissionCodes)
	}
}

// TestAdminMenuListDirectAndEffective 用户菜单 ID：直接权限 + 角色继承去重。
func TestAdminMenuListDirectAndEffective(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	codeA := "am_a_" + uniq("")
	codeB := "am_b_" + uniq("")
	createPerm(t, e, codeA, "/api/am_a")
	createPerm(t, e, codeB, "/api/am_b")

	// 两个菜单（type=2）
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "AM菜单A", Type: adminmodel.MenuTypeMenu, Path: "/am/a",
		Component: "view.amA", PermissionCode: codeA, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menuA adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "AM菜单A").First(&menuA).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "AM菜单B", Type: adminmodel.MenuTypeMenu, Path: "/am/b",
		Component: "view.amB", PermissionCode: codeB, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menuB adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "AM菜单B").First(&menuB).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	userID := uint64(777)
	// 角色继承 A
	roleID := createRole(t, e, "am_role_"+uniq(""), "AM角色")
	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menuA.ID}}); err != nil {
		t.Fatalf("角色授权失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserRoleBindings(idStr(userID), []string{codeOfRole(t, e, roleID)}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}
	// 用户直接权限 B
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

// TestAdminRoleSaveAndList 用户角色绑定保存与查询。
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

// TestBuildAuthorizedTree 授权树：仅授权/公开菜单可见，按钮/外链不进入树，祖先自动补齐。
// 已知缺陷复现：内部同样走 buildMenuTree（值拷贝 roots），授权目录的子节点丢失
// → 本测试预期「目录下应有授权菜单」会失败（与 TestMenuTreeBuild 同一缺陷）。
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
		Component: "view.authzM", PermissionCode: code, ParentID: dir.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	// 未授权菜单绑定另一个权限（不在授权 codes 中）
	otherCode := "tree_noauth_" + uniq("")
	createPerm(t, e, otherCode, "/api/noauth")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "未授权菜单", Type: adminmodel.MenuTypeMenu, Path: "/noauth",
		Component: "view.noAuth", PermissionCode: otherCode, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}

	tree, err := e.svc.BuildAuthorizedTree(ctx, []string{code})
	wantErr(t, err, "")
	if len(tree) != 1 {
		t.Fatalf("应只有授权目录一个根: %d", len(tree))
	}
	if tree[0].Title != "授权目录" || len(tree[0].Children) != 1 || tree[0].Children[0].Title != "授权菜单" {
		t.Fatalf("授权树结构不符: %+v", tree)
	}
}
