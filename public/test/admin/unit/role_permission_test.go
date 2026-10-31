package unit

import (
	"context"
	"sort"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func createPermMenu(t *testing.T, e *env, parent uint64, typ int, code string) uint64 {
	t.Helper()
	req := &admindto.MenuCreateReq{
		Title:     "权限树 " + uniq(""),
		ParentID:  parent,
		Type:      typ,
		Status:    1,
		SortOrder: 1,
	}
	if code != "" {
		req.PermissionCodes = []string{code}
	}

	if typ == adminmodel.MenuTypeMenu {
		req.Path = "/test/perm-tree"
	}
	wantErr(t, e.svc.MenuCreate(context.Background(), req), "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", req.Title).First(&menu).Error; err != nil {
		t.Fatalf("查询刚建的菜单失败: %v", err)
	}
	return menu.ID
}

func rolePermissionCodes(t *testing.T, e *env, roleID uint64) []string {
	t.Helper()
	role, err := e.dbGetRole(roleID)
	wantErr(t, err, "")
	perms, err := pkgcasbin.GetRolePermissions(role.RoleCode)
	wantErr(t, err, "")
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, p[2])
	}
	sort.Strings(out)
	return out
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameUint64(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func findMenuNodeID(nodes []admindto.MenuTreeNode, id uint64) *admindto.MenuTreeNode {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if found := findMenuNodeID(nodes[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

func TestRoleMenuSaveCompletesAncestorMenus(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	menuCode := "rp_menu_" + uniq("")
	btnCode := "rp_btn_" + uniq("")
	createPerm(t, e, menuCode, "/api/rp/menu")
	createPerm(t, e, btnCode, "/api/rp/btn")

	createPerm(t, e, "bg_"+uniq(""), "/api/background")

	dirID := createPermMenu(t, e, 0, adminmodel.MenuTypeDirectory, "")
	menuID := createPermMenu(t, e, dirID, adminmodel.MenuTypeMenu, menuCode)
	btnID := createPermMenu(t, e, menuID, adminmodel.MenuTypeButton, btnCode)

	roleID := createRole(t, e, "rp_"+uniq(""), "权限角色")

	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{btnID}}); err != nil {
		t.Fatalf("保存角色权限失败: %v", err)
	}

	wantCodes := []string{btnCode, menuCode}
	sort.Strings(wantCodes)
	if got := rolePermissionCodes(t, e, roleID); !sameStrings(got, wantCodes) {
		t.Fatalf("祖先菜单的权限码没有一起落库: got=%v want=%v", got, wantCodes)
	}

	list, err := e.svc.RoleMenuList(ctx, &admindto.RoleMenuListReq{RoleID: roleID})
	wantErr(t, err, "")
	if want := []uint64{dirID, menuID, btnID}; !sameUint64(list.MenuIDs, want) {
		t.Fatalf("角色菜单反查没有补齐祖先: got=%v want=%v", list.MenuIDs, want)
	}

	tree, err := e.svc.RolePermissionTree(ctx, roleID)
	wantErr(t, err, "")
	if want := []uint64{dirID, menuID, btnID}; !sameUint64(tree.MenuIDs, want) {
		t.Fatalf("分配树勾选集合不符: got=%v want=%v", tree.MenuIDs, want)
	}
	for _, id := range []uint64{dirID, menuID, btnID} {
		if findMenuNodeID(tree.Tree, id) == nil {
			t.Fatalf("分配树里缺少节点 %d（树必须能渲染出每一个勾选项）", id)
		}
	}
}

func TestRoleMenuSaveUnknownMenuIDsIgnored(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "rp_unk_" + uniq("")
	createPerm(t, e, code, "/api/rp/unk")
	createPerm(t, e, "bg_"+uniq(""), "/api/background")
	menuID := createPermMenu(t, e, 0, adminmodel.MenuTypeMenu, code)
	roleID := createRole(t, e, "rpu_"+uniq(""), "未知 id 角色")

	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{
		RoleID: roleID, MenuIDs: []uint64{menuID, 999999999},
	}); err != nil {
		t.Fatalf("混入未知 id 不应报错: %v", err)
	}

	if got := rolePermissionCodes(t, e, roleID); !sameStrings(got, []string{code}) {
		t.Fatalf("未知 id 不应产生任何策略: got=%v", got)
	}
	list, err := e.svc.RoleMenuList(ctx, &admindto.RoleMenuListReq{RoleID: roleID})
	wantErr(t, err, "")
	if !sameUint64(list.MenuIDs, []uint64{menuID}) {
		t.Fatalf("未知 id 不应出现在反查结果里: got=%v", list.MenuIDs)
	}
}

func TestRoleMenuSaveEmptyClearsAllPermissions(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "rp_clr_" + uniq("")
	createPerm(t, e, code, "/api/rp/clr")
	createPerm(t, e, "bg_"+uniq(""), "/api/background")
	menuID := createPermMenu(t, e, 0, adminmodel.MenuTypeMenu, code)
	roleID := createRole(t, e, "rpc_"+uniq(""), "清空角色")

	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menuID}}); err != nil {
		t.Fatalf("先授予权限失败: %v", err)
	}
	if got := rolePermissionCodes(t, e, roleID); len(got) != 1 {
		t.Fatalf("预置授权失败: got=%v", got)
	}

	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID}); err != nil {
		t.Fatalf("清空角色权限失败: %v", err)
	}
	if got := rolePermissionCodes(t, e, roleID); len(got) != 0 {
		t.Fatalf("空提交应清空全部策略: got=%v", got)
	}

	list, err := e.svc.RoleMenuList(ctx, &admindto.RoleMenuListReq{RoleID: roleID})
	wantErr(t, err, "")
	if len(list.MenuIDs) != 0 {
		t.Fatalf("清空后不应再反查出菜单: got=%v", list.MenuIDs)
	}
}

func TestRolePermissionTreeKeepsCheckedButDisabledMenu(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "rp_dis_" + uniq("")
	createPerm(t, e, code, "/api/rp/dis")
	createPerm(t, e, "bg_"+uniq(""), "/api/background")
	menuID := createPermMenu(t, e, 0, adminmodel.MenuTypeMenu, code)
	roleID := createRole(t, e, "rpd_"+uniq(""), "禁用回归角色")

	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{menuID}}); err != nil {
		t.Fatalf("授予权限失败: %v", err)
	}

	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ?", menuID).
		Update("status", adminmodel.MenuStatusDisabled).Error; err != nil {
		t.Fatalf("禁用菜单失败: %v", err)
	}

	tree, err := e.svc.RolePermissionTree(ctx, roleID)
	wantErr(t, err, "")
	if !sameUint64(tree.MenuIDs, []uint64{menuID}) {
		t.Fatalf("已勾选但被禁用的菜单从勾选集合里丢了: got=%v", tree.MenuIDs)
	}
	if findMenuNodeID(tree.Tree, menuID) == nil {
		t.Fatalf("已勾选但被禁用的菜单不在树里（保存时会静默丢掉这条授权）: MenuIDs=%v", tree.MenuIDs)
	}
}
