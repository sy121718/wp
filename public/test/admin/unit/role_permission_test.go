package unit

import (
	"context"
	"sort"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

// role_permission_test.go — 角色权限分配（角色分权）的数据库侧回归。
//
// 覆盖的是「服务端必须自己成立的不变式」，不是页面的排版：
//  1. 只提交子节点（按钮）时，祖先菜单的权限码要一起落库，反查与分配树要含目录；
//  2. 提交空集合 = 清空该角色全部权限（这是合法提交，不是「没选，忽略」）；
//  3. 勾选之后才被禁用的菜单，仍要留在分配树里（否则一保存就静默丢授权）。
//
// 这些用例都用真实 PostgreSQL（support.NewMigratedPGTestDB），因为要断言的正是
// 「sys_casbin_rule 里到底写了什么」—— 用假实现测这一段等于什么都没测。

// createPermMenu 建一个带层级与权限码的菜单节点，返回 id。
// 用 req.Title 精确定位刚建的那一行（uniq 后缀保证不撞其它用例建的节点）。
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
	// 菜单类型必须绑组件（目录与按钮不绑）：与生产写入侧的校验一致。
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

// TestRoleMenuSaveCompletesAncestorMenus 只勾按钮时，祖先菜单的权限码必须一起落库。
func TestRoleMenuSaveCompletesAncestorMenus(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	menuCode := "rp_menu_" + uniq("")
	btnCode := "rp_btn_" + uniq("")
	createPerm(t, e, menuCode, "/api/rp/menu")
	createPerm(t, e, btnCode, "/api/rp/btn")
	// 拉高「覆盖全部启用权限点」的判定门槛：测试环境权限点全集很小，
	// 只授一两个会被超管保护误判成「目标角色将变成超管等价角色」。
	createPerm(t, e, "bg_"+uniq(""), "/api/background")

	dirID := createPermMenu(t, e, 0, adminmodel.MenuTypeDirectory, "")
	menuID := createPermMenu(t, e, dirID, adminmodel.MenuTypeMenu, menuCode)
	btnID := createPermMenu(t, e, menuID, adminmodel.MenuTypeButton, btnCode)

	roleID := createRole(t, e, "rp_"+uniq(""), "权限角色")

	// 只提交按钮 id：模拟「前端没补祖先」与「禁用 JS 时用户只勾了按钮」两种情形。
	// 服务端必须自己补齐 —— 这是「子勾 ⇒ 父勾」不变式的落点。
	if _, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: roleID, MenuIDs: []uint64{btnID}}); err != nil {
		t.Fatalf("保存角色权限失败: %v", err)
	}

	wantCodes := []string{btnCode, menuCode}
	sort.Strings(wantCodes)
	if got := rolePermissionCodes(t, e, roleID); !sameStrings(got, wantCodes) {
		t.Fatalf("祖先菜单的权限码没有一起落库: got=%v want=%v", got, wantCodes)
	}

	// 反查必须含目录：目录没有权限码，只能靠向上补齐得到 —— 没有它，分配页每次打开都会
	// 显示目录未勾选，被当成「上次勾的没生效」。
	list, err := e.svc.RoleMenuList(ctx, &admindto.RoleMenuListReq{RoleID: roleID})
	wantErr(t, err, "")
	if want := []uint64{dirID, menuID, btnID}; !sameUint64(list.MenuIDs, want) {
		t.Fatalf("角色菜单反查没有补齐祖先: got=%v want=%v", list.MenuIDs, want)
	}

	// 分配树：页面直接按 MenuIDs 渲染 checked / 按 Tree 渲染行，两者都不能缺。
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

// TestRoleMenuSaveUnknownMenuIDsIgnored 提交里混着不存在的 id 时不能报错，也不能落库。
//
// 请求体不受信任：前端提交的是整棵勾选树，其中任何 id 都可能被篡改。白名单在
// withAncestorMenuIDs 里（不在 sys_menus 的 id 直接丢弃），这里把那条边界钉住。
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

// TestRoleMenuSaveEmptyClearsAllPermissions 提交空集合 = 清空该角色全部权限。
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

	// 「一个都没勾」是合法提交（清空），不能当成「没选，忽略本次提交」——
	// 后者会让运营永远清不掉一个多余角色上的权限。
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

// TestRolePermissionTreeKeepsCheckedButDisabledMenu 勾选之后才被禁用的菜单仍留在分配树里。
//
// 这是「静默丢授权」的唯一防线：树里没有它 → 页面渲染不出这个勾选 →
// 管理员在页面上随手一点保存，这条策略就被删掉了，而页面上什么都没显示。
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

	// 之后菜单被禁用（纯管理动作，与角色授权无关）。
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
