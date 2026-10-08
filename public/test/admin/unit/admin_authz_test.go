package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

// TestEffectivePermissionCodesMergesRoleAndDirect 有效权限码：角色继承 + 直接权限合并去重。
//
// 这条判据原先挂在已删除的 GET /api/admin/routes（Vue 时代的动态路由投影）上。
// 那个端点的唯一真实用途是「算有效权限码」，而这件事现在由 EffectivePermissionCodes
// 独立承担（侧栏菜单过滤与按钮显隐都读它）—— 判据随之搬到函数上，覆盖不减。
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

	// 角色绑定菜单权限 + 用户绑定角色（继承侧）
	roleCode := "eff_role_" + uniq("")
	roleID := createRole(t, e, roleCode, "有效码角色")
	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menu.ID}}); err != nil {
		t.Fatalf("角色授权失败: %v", err)
	}
	userID := uint64(5252)
	if err := pkgcasbin.ReplaceUserRoleBindings(idStr(userID), []string{roleCode}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}
	// 用户直接权限（直接侧）
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
	// 去重：两个来源都授权同一个码时只出现一次
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
		PermissionCodes: []string{code}, ParentID: dir.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	// 未授权菜单绑定另一个权限（不在授权 codes 中）
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
	// 同样不断言「恰好一个根」：迁移会在库里 seed 目录（229 的「库存」，其下的
	// 「仓库管理」「变动原因字典」标了 is_public=1，任何登录用户都能看见），
	// 根的数量不是本测试的契约。本测试守的是「按授权过滤」这两件事：
	// ① 授权目录在且结构正确；② 未授权的那条菜单不出现。
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

// findMenuNodeByTitle 在菜单树里按标题定位节点（同上：不依赖「根只有一个」）。
func findMenuNodeByTitle(nodes []admindto.MenuTreeNode, title string) *admindto.MenuTreeNode {
	for i := range nodes {
		if nodes[i].Title == title {
			return &nodes[i]
		}
	}
	return nil
}

// hasMenuNodeTitle 递归查找树里是否出现指定标题（断言「未授权节点不出现」用）。
func hasMenuNodeTitle(nodes []admindto.MenuTreeNode, title string) bool {
	for _, n := range nodes {
		if n.Title == title || hasMenuNodeTitle(n.Children, title) {
			return true
		}
	}
	return false
}
