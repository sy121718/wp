package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

// --- 权限点 ---

// TestPermCreateSuccess 权限点创建成功，默认启用。
func TestPermCreateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm:" + uniq("")
	res, err := e.svc.PermCreate(ctx, &admindto.PermCreateReq{
		PermissionCode: code, PermissionName: "列表", Module: "admin",
		APIPath: "/api/admin/list", APIMethod: "GET", Status: 1,
	})
	wantErr(t, err, "")
	if res.ID == 0 {
		t.Fatalf("权限点 ID 不应为 0")
	}

	var perm adminmodel.PermissionEntity
	if err := e.db.First(&perm, res.ID).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if perm.Status != adminmodel.PermissionStatusEnabled {
		t.Fatalf("权限点应启用: got=%d", perm.Status)
	}
}

// TestPermCreateDuplicateCode 重复权限编码被拒绝。
func TestPermCreateDuplicateCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_dup:" + uniq("")
	_, err := e.svc.PermCreate(ctx, &admindto.PermCreateReq{
		PermissionCode: code, PermissionName: "A", Module: "admin", APIPath: "/a", APIMethod: "GET",
	})
	wantErr(t, err, "")
	_, err = e.svc.PermCreate(ctx, &admindto.PermCreateReq{
		PermissionCode: code, PermissionName: "B", Module: "admin", APIPath: "/b", APIMethod: "GET",
	})
	wantErr(t, err, adminenums.ErrCodeExists)
}

// TestPermUpdateSuccess 更新权限点名称与路径。
func TestPermUpdateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_upd:" + uniq("")
	permID := createPerm(t, e, code, "/api/orig")
	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "新名称", Module: "admin",
		APIPath: "/api/new", APIMethod: "POST", Status: 1,
	})
	wantErr(t, err, "")

	detail, err := e.svc.PermDetail(ctx, &admindto.PermDetailReq{ID: permID})
	wantErr(t, err, "")
	if detail.PermissionName != "新名称" || detail.APIPath != "/api/new" || detail.APIMethod != "POST" {
		t.Fatalf("权限点更新不符: %+v", detail)
	}
}

// TestPermUpdateCodeImmutable 权限编码创建后不可修改。
func TestPermUpdateCodeImmutable(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	permID := createPerm(t, e, "perm_imm:"+uniq(""), "/api/imm")
	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: "perm_changed:" + uniq(""), PermissionName: "x",
		Module: "admin", APIPath: "/api/imm", APIMethod: "GET", Status: 1,
	})
	wantErr(t, err, adminenums.ErrCodeImmutable)
}

// TestPermUpdateDisableAssignedRejected 已分配权限禁止禁用。
func TestPermUpdateDisableAssignedRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_assigned:" + uniq("")
	permID := createPerm(t, e, code, "/api/assigned")
	if err := pkgcasbin.ReplaceRolePermissions("some_role", [][3]string{{"/api/assigned", "GET", code}}); err != nil {
		t.Fatalf("写入 Casbin 分配失败: %v", err)
	}

	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "x", Module: "admin",
		APIPath: "/api/assigned", APIMethod: "GET", Status: adminmodel.PermissionStatusDisabled,
	})
	wantErr(t, err, adminenums.ErrPermissionAssigned)
}

// TestPermUpdateDisablePersists 未分配权限点显式更新为 status=0（禁用）落库为禁用。
// 修复前：PermissionModel.Update 用裸 Updates(struct)，零值 status=0 被跳过，
// 禁用不落库，权限点保持启用（同 SysRuleEntity.Status default tag 一族的改写问题）。
func TestPermUpdateDisablePersists(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_dis:" + uniq("")
	permID := createPerm(t, e, code, "/api/dis")
	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "禁用权限", Module: "admin",
		APIPath: "/api/dis", APIMethod: "GET", Status: adminmodel.PermissionStatusDisabled,
	})
	wantErr(t, err, "")

	var perm adminmodel.PermissionEntity
	if err := e.db.First(&perm, permID).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if perm.Status != adminmodel.PermissionStatusDisabled {
		t.Fatalf("显式禁用应落库 status=0: got=%d", perm.Status)
	}
}

// TestPermUpdateDefinitionSyncCasbin 更新已分配权限的 path/method 会同步 Casbin 策略。
func TestPermUpdateDefinitionSyncCasbin(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_sync:" + uniq("")
	permID := createPerm(t, e, code, "/api/old")
	if err := pkgcasbin.ReplaceRolePermissions("sync_role", [][3]string{{"/api/old", "GET", code}}); err != nil {
		t.Fatalf("写入 Casbin 分配失败: %v", err)
	}

	_, err := e.svc.PermUpdate(ctx, &admindto.PermUpdateReq{
		ID: permID, PermissionCode: code, PermissionName: "x", Module: "admin",
		APIPath: "/api/new", APIMethod: "POST", Status: 1,
	})
	wantErr(t, err, "")

	perms, err := pkgcasbin.GetRolePermissions("sync_role")
	wantErr(t, err, "")
	if len(perms) != 1 || perms[0][0] != "/api/new" || perms[0][1] != "POST" {
		t.Fatalf("Casbin 策略未同步: %v", perms)
	}
}

// TestPermDeleteSuccess 删除未分配、未被菜单引用的权限点。
func TestPermDeleteSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	permID := createPerm(t, e, "perm_del:"+uniq(""), "/api/del")
	res, err := e.svc.PermDelete(ctx, &admindto.PermDeleteReq{IDs: []uint64{permID}})
	wantErr(t, err, "")
	if res.DeletedCount != 1 {
		t.Fatalf("删除数量不符: got=%d", res.DeletedCount)
	}
}

// TestPermDeleteNotFound 批量删除存在不存在的 ID 时报错。
func TestPermDeleteNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	permID := createPerm(t, e, "perm_nf:"+uniq(""), "/api/nf")
	_, err := e.svc.PermDelete(ctx, &admindto.PermDeleteReq{IDs: []uint64{permID, 999999}})
	wantErr(t, err, adminenums.ErrPermissionNotFound)
}

// TestPermDeleteAssignedRejected 已分配权限禁止删除。
func TestPermDeleteAssignedRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_del_assigned:" + uniq("")
	permID := createPerm(t, e, code, "/api/da")
	if err := pkgcasbin.ReplaceRolePermissions("da_role", [][3]string{{"/api/da", "GET", code}}); err != nil {
		t.Fatalf("写入 Casbin 分配失败: %v", err)
	}
	_, err := e.svc.PermDelete(ctx, &admindto.PermDeleteReq{IDs: []uint64{permID}})
	wantErr(t, err, adminenums.ErrPermissionAssigned)
}

// TestPermDeleteMenuReferencedRejected 被菜单引用的权限禁止删除。
func TestPermDeleteMenuReferencedRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_ref:" + uniq("")
	permID := createPerm(t, e, code, "/api/ref")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "引用菜单", Type: adminmodel.MenuTypeMenu, Path: "/ref",
		Component: "view.refMenu", PermissionCode: code, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	_, err := e.svc.PermDelete(ctx, &admindto.PermDeleteReq{IDs: []uint64{permID}})
	wantErr(t, err, adminenums.ErrMenuReferenced)
}

// TestPermDetailNotFound 查询不存在的权限点报错。
func TestPermDetailNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.PermDetail(ctx, &admindto.PermDetailReq{ID: 555555})
	wantErr(t, err, adminenums.ErrPermissionNotFound)
}

// TestPermListFilters 权限点列表筛选。
func TestPermListFilters(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	createPerm(t, e, "pl_mod_a:"+uniq(""), "/api/a")
	createPerm(t, e, "pl_mod_b:"+uniq(""), "/api/b")

	res, err := e.svc.PermList(ctx, &admindto.PermListReq{Module: "admin"})
	wantErr(t, err, "")
	if res.Total != 2 {
		t.Fatalf("模块筛选不符: total=%d", res.Total)
	}
	res, err = e.svc.PermList(ctx, &admindto.PermListReq{Code: "pl_mod_a"})
	wantErr(t, err, "")
	if res.Total != 1 {
		t.Fatalf("编码筛选不符: total=%d", res.Total)
	}
}

// --- 菜单 ---

// TestMenuCreateSuccess 菜单类型（type=2）创建成功。
func TestMenuCreateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu:" + uniq("")
	createPerm(t, e, code, "/api/menu")
	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "用户管理", Type: adminmodel.MenuTypeMenu, Path: "/system/user",
		Component: "view.systemUser", PermissionCode: code, Status: 1, SortOrder: 3,
	})
	wantErr(t, err, "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "用户管理").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if menu.Component != "view.systemUser" || menu.PermissionCode == nil || *menu.PermissionCode != code {
		t.Fatalf("菜单字段不符: %+v", menu)
	}
}

// TestMenuCreateDirectoryRejectsComponent 目录类型不允许填写组件路径。
func TestMenuCreateDirectoryRejectsComponent(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "目录", Type: adminmodel.MenuTypeDirectory, Path: "/dir", Component: "layout.base",
	})
	wantErr(t, err, adminenums.ErrComponentNotAllowed)
}

// TestMenuCreateMenuRequiresComponent 菜单类型必须填写组件路径。
func TestMenuCreateMenuRequiresComponent(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "菜单", Type: adminmodel.MenuTypeMenu, Path: "/m", Component: "",
	})
	wantErr(t, err, adminenums.ErrComponentRequired)
}

// TestMenuCreateInvalidComponent 非法组件格式被拒绝。
func TestMenuCreateInvalidComponent(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "坏组件", Type: adminmodel.MenuTypeMenu, Path: "/bad",
		Component: "evil/../etc/passwd",
	})
	wantErr(t, err, adminenums.ErrComponentInvalid)
}

// TestMenuCreateDirectoryRejectsCode 目录/iframe/外链不允许绑定权限编码。
func TestMenuCreateDirectoryRejectsCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "目录带码", Type: adminmodel.MenuTypeDirectory, Path: "/dir",
		PermissionCode: "any:code",
	})
	wantErr(t, err, adminenums.ErrCodeNotBindable)
}

// TestMenuCreateMenuRequiresCode 菜单类型必须绑定权限编码。
func TestMenuCreateMenuRequiresCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "无码菜单", Type: adminmodel.MenuTypeMenu, Path: "/nocode",
		Component: "view.noCode",
	})
	wantErr(t, err, adminenums.ErrCodeRequired)
}

// TestMenuCreateCodeNotEnabled 绑定不存在的权限编码被拒绝。
func TestMenuCreateCodeNotEnabled(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "坏码菜单", Type: adminmodel.MenuTypeMenu, Path: "/badcode",
		Component: "view.badCode", PermissionCode: "no_such_code_xyz",
	})
	wantErr(t, err, adminenums.ErrCodeNotEnabled)
}

// TestMenuUpdateCircleSelf 菜单不能挂到自己下面。
func TestMenuUpdateCircleSelf(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_circle:" + uniq("")
	createPerm(t, e, code, "/api/circle")
	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "环菜单", Type: adminmodel.MenuTypeMenu, Path: "/circle",
		Component: "view.circle", PermissionCode: code, Status: 1,
	})
	wantErr(t, err, "")
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "环菜单").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	err = e.svc.MenuUpdate(ctx, &admindto.MenuUpdateReq{
		ID: menu.ID, Title: "环菜单", Type: adminmodel.MenuTypeMenu, Path: "/circle",
		Component: "view.circle", PermissionCode: code, ParentID: menu.ID, Status: 1,
	})
	wantErr(t, err, adminenums.ErrMenuCircle)
}

// TestMenuUpdateCircleDescendant 菜单不能挂到自己的子孙下面。
func TestMenuUpdateCircleDescendant(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_cir2:" + uniq("")
	createPerm(t, e, code, "/api/cir2")
	// 父菜单
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "父", Type: adminmodel.MenuTypeMenu, Path: "/p", Component: "view.p", PermissionCode: code, Status: 1,
	}); err != nil {
		t.Fatalf("创建父菜单失败: %v", err)
	}
	var parent adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "父").First(&parent).Error; err != nil {
		t.Fatalf("查询父菜单失败: %v", err)
	}
	// 子菜单挂在父下
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "子", Type: adminmodel.MenuTypeMenu, Path: "/c", Component: "view.c",
		PermissionCode: code, ParentID: parent.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}
	var child adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "子").First(&child).Error; err != nil {
		t.Fatalf("查询子菜单失败: %v", err)
	}

	// 把父挂到子下面 → 环
	err := e.svc.MenuUpdate(ctx, &admindto.MenuUpdateReq{
		ID: parent.ID, Title: "父", Type: adminmodel.MenuTypeMenu, Path: "/p",
		Component: "view.p", PermissionCode: code, ParentID: child.ID, Status: 1,
	})
	wantErr(t, err, adminenums.ErrMenuCircle)
}

// TestMenuUpdateSystemTypeImmutable 系统内置菜单不允许修改类型。
func TestMenuUpdateSystemTypeImmutable(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_sys:" + uniq("")
	createPerm(t, e, code, "/api/sys")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "系统菜单", Type: adminmodel.MenuTypeMenu, Path: "/sys",
		Component: "view.sys", PermissionCode: code, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "系统菜单").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ?", menu.ID).Update("is_system", 1).Error; err != nil {
		t.Fatalf("设置系统菜单失败: %v", err)
	}

	err := e.svc.MenuUpdate(ctx, &admindto.MenuUpdateReq{
		ID: menu.ID, Title: "系统菜单", Type: adminmodel.MenuTypeDirectory, Path: "/sys",
		Component: "", PermissionCode: code, Status: 1,
	})
	wantErr(t, err, adminenums.ErrMenuIsSystem)
}

// TestMenuDeleteHasChildren 有子菜单的菜单不可删除。
func TestMenuDeleteHasChildren(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_children:" + uniq("")
	createPerm(t, e, code, "/api/ch")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "父菜单", Type: adminmodel.MenuTypeMenu, Path: "/pc", Component: "view.pc", PermissionCode: code, Status: 1,
	}); err != nil {
		t.Fatalf("创建父菜单失败: %v", err)
	}
	var parent adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "父菜单").First(&parent).Error; err != nil {
		t.Fatalf("查询父菜单失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "子菜单", Type: adminmodel.MenuTypeMenu, Path: "/cc", Component: "view.cc",
		PermissionCode: code, ParentID: parent.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}

	err := e.svc.MenuDelete(ctx, &admindto.MenuDeleteReq{IDs: []uint64{parent.ID}})
	wantErr(t, err, adminenums.ErrMenuHasChildren)
}

// TestMenuDeleteSystemRejected 系统菜单不可删除。
func TestMenuDeleteSystemRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_sysdel:" + uniq("")
	createPerm(t, e, code, "/api/sysdel")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "待删系统菜单", Type: adminmodel.MenuTypeMenu, Path: "/sd",
		Component: "view.sd", PermissionCode: code, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "待删系统菜单").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ?", menu.ID).Update("is_system", 1).Error; err != nil {
		t.Fatalf("设置系统菜单失败: %v", err)
	}

	err := e.svc.MenuDelete(ctx, &admindto.MenuDeleteReq{IDs: []uint64{menu.ID}})
	wantErr(t, err, adminenums.ErrMenuIsSystem)
}

// TestMenuTreeBuild 菜单树组装：父子挂载、孤儿提升为顶级。
// 已知缺陷复现：buildMenuTree 把顶级节点以值拷贝放入 roots，而子节点挂载发生在
// nodeMap 指针上——默认排序（sort_order ASC, id ASC）下父节点先处理，
// roots 中的父节点副本 Children 恒为空 → 本测试预期「目录下应有 1 个子节点」会失败。
func TestMenuTreeBuild(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_tree:" + uniq("")
	createPerm(t, e, code, "/api/tree")
	// 顶级目录
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "顶级目录", Type: adminmodel.MenuTypeDirectory, Path: "/top", Status: 1,
	}); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	var dir adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "顶级目录").First(&dir).Error; err != nil {
		t.Fatalf("查询目录失败: %v", err)
	}
	// 子菜单
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "子菜单X", Type: adminmodel.MenuTypeMenu, Path: "/sub",
		Component: "view.subX", PermissionCode: code, ParentID: dir.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}
	// 孤儿（父不存在）
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "孤儿菜单", Type: adminmodel.MenuTypeMenu, Path: "/orphan",
		Component: "view.orphan", PermissionCode: code, ParentID: 987654, Status: 1,
	}); err != nil {
		t.Fatalf("创建孤儿菜单失败: %v", err)
	}

	tree, err := e.svc.MenuTree(ctx, &admindto.MenuTreeReq{})
	wantErr(t, err, "")
	if len(tree) != 2 {
		t.Fatalf("应有两个顶级（目录 + 孤儿）: %d", len(tree))
	}
	var dirNode *admindto.MenuTreeNode
	for i := range tree {
		if tree[i].Title == "顶级目录" {
			dirNode = &tree[i]
		}
	}
	if dirNode == nil {
		t.Fatalf("未找到顶级目录节点: %+v", tree)
	}
	if len(dirNode.Children) != 1 || dirNode.Children[0].Title != "子菜单X" {
		t.Fatalf("目录子节点不符: %+v", dirNode.Children)
	}
}

// TestMenuSoftDelete 软删除后不再出现在列表与树中。
func TestMenuSoftDelete(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_soft:" + uniq("")
	createPerm(t, e, code, "/api/soft")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "待软删", Type: adminmodel.MenuTypeMenu, Path: "/soft",
		Component: "view.soft", PermissionCode: code, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "待软删").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}

	err := e.svc.MenuDelete(ctx, &admindto.MenuDeleteReq{IDs: []uint64{menu.ID}})
	wantErr(t, err, "")

	var count int64
	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ? AND deleted_time IS NULL", menu.ID).Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("软删除后应不可见")
	}
	// 原始行仍在（软删除语义）
	var rawCount int64
	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ?", menu.ID).Count(&rawCount).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if rawCount != 1 {
		t.Fatalf("软删除应保留原始行")
	}
}
