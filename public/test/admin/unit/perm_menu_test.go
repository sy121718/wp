package unit

import (
	"context"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

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

func TestPermDeleteNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	permID := createPerm(t, e, "perm_nf:"+uniq(""), "/api/nf")
	_, err := e.svc.PermDelete(ctx, &admindto.PermDeleteReq{IDs: []uint64{permID, 999999}})
	wantErr(t, err, adminenums.ErrPermissionNotFound)
}

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

func TestPermDeleteMenuReferencedRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "perm_ref:" + uniq("")
	permID := createPerm(t, e, code, "/api/ref")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "引用菜单", Type: adminmodel.MenuTypeMenu, Path: "/ref",
		PermissionCodes: []string{code}, Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	_, err := e.svc.PermDelete(ctx, &admindto.PermDeleteReq{IDs: []uint64{permID}})
	wantErr(t, err, adminenums.ErrMenuReferenced)
}

func TestPermDetailNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.PermDetail(ctx, &admindto.PermDetailReq{ID: 555555})
	wantErr(t, err, adminenums.ErrPermissionNotFound)
}

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

func TestMenuCreateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu:" + uniq("")
	createPerm(t, e, code, "/api/menu")
	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "用户管理", Type: adminmodel.MenuTypeMenu, Path: "/system/user",
		PermissionCodes: []string{code}, Status: 1, SortOrder: 3,
	})
	wantErr(t, err, "")

	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "用户管理").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if menu.Path != "/system/user" {
		t.Fatalf("菜单字段不符: %+v", menu)
	}

	var linked []string
	if err := e.db.Table("sys_menu_permission").Where("menu_id = ?", menu.ID).
		Pluck("permission_code", &linked).Error; err != nil {
		t.Fatalf("查询菜单权限关联失败: %v", err)
	}
	if len(linked) != 1 || linked[0] != code {
		t.Fatalf("菜单权限关联不符: %v（期望 [%s]）", linked, code)
	}
	if menu.PermissionCode != nil {
		t.Fatalf("界面写入不应再改旧列 permission_code，实际为 %q", *menu.PermissionCode)
	}
}

func TestMenuCreateDirectoryRejectsCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "目录带码", Type: adminmodel.MenuTypeDirectory, Path: "/dir",
		PermissionCodes: []string{"any:code"},
	})
	wantErr(t, err, adminenums.ErrCodeNotBindable)
}

func TestMenuCreateMenuRequiresCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "无码菜单", Type: adminmodel.MenuTypeMenu, Path: "/nocode",
	})
	wantErr(t, err, adminenums.ErrCodeRequired)
}

func TestMenuCreateCodeNotEnabled(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "坏码菜单", Type: adminmodel.MenuTypeMenu, Path: "/badcode", PermissionCodes: []string{"no_such_code_xyz"},
	})
	wantErr(t, err, adminenums.ErrCodeNotEnabled)
}

func TestMenuUpdateCircleSelf(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_circle:" + uniq("")
	createPerm(t, e, code, "/api/circle")
	err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "环菜单", Type: adminmodel.MenuTypeMenu, Path: "/circle", PermissionCodes: []string{code}, Status: 1,
	})
	wantErr(t, err, "")
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "环菜单").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	err = e.svc.MenuUpdate(ctx, &admindto.MenuUpdateReq{
		ID: menu.ID, Title: "环菜单", Type: adminmodel.MenuTypeMenu, Path: "/circle", PermissionCodes: []string{code}, ParentID: menu.ID, Status: 1,
	})
	wantErr(t, err, adminenums.ErrMenuCircle)
}

func TestMenuUpdateCircleDescendant(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_cir2:" + uniq("")
	createPerm(t, e, code, "/api/cir2")

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "父", Type: adminmodel.MenuTypeMenu, Path: "/p", PermissionCodes: []string{code}, Status: 1,
	}); err != nil {
		t.Fatalf("创建父菜单失败: %v", err)
	}
	var parent adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "父").First(&parent).Error; err != nil {
		t.Fatalf("查询父菜单失败: %v", err)
	}

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "子", Type: adminmodel.MenuTypeMenu, Path: "/c",
		PermissionCodes: []string{code}, ParentID: parent.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}
	var child adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "子").First(&child).Error; err != nil {
		t.Fatalf("查询子菜单失败: %v", err)
	}

	err := e.svc.MenuUpdate(ctx, &admindto.MenuUpdateReq{
		ID: parent.ID, Title: "父", Type: adminmodel.MenuTypeMenu, Path: "/p", PermissionCodes: []string{code}, ParentID: child.ID, Status: 1,
	})
	wantErr(t, err, adminenums.ErrMenuCircle)
}

func TestMenuUpdateSystemTypeImmutable(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_sys:" + uniq("")
	createPerm(t, e, code, "/api/sys")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "系统菜单", Type: adminmodel.MenuTypeMenu, Path: "/sys", PermissionCodes: []string{code}, Status: 1,
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
		ID: menu.ID, Title: "系统菜单", Type: adminmodel.MenuTypeDirectory, Path: "/sys", PermissionCodes: []string{code}, Status: 1,
	})
	wantErr(t, err, adminenums.ErrMenuIsSystem)
}

func TestMenuDeleteHasChildren(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_children:" + uniq("")
	createPerm(t, e, code, "/api/ch")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "父菜单", Type: adminmodel.MenuTypeMenu, Path: "/pc", PermissionCodes: []string{code}, Status: 1,
	}); err != nil {
		t.Fatalf("创建父菜单失败: %v", err)
	}
	var parent adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "父菜单").First(&parent).Error; err != nil {
		t.Fatalf("查询父菜单失败: %v", err)
	}
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "子菜单", Type: adminmodel.MenuTypeMenu, Path: "/cc",
		PermissionCodes: []string{code}, ParentID: parent.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}

	err := e.svc.MenuDelete(ctx, &admindto.MenuDeleteReq{IDs: []uint64{parent.ID}})
	wantErr(t, err, adminenums.ErrMenuHasChildren)
}

func TestMenuDeleteSystemRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_sysdel:" + uniq("")
	createPerm(t, e, code, "/api/sysdel")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "待删系统菜单", Type: adminmodel.MenuTypeMenu, Path: "/sd", PermissionCodes: []string{code}, Status: 1,
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

func TestMenuTreeBuild(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	if err := e.db.Exec("DELETE FROM sys_menus").Error; err != nil {
		t.Fatalf("清理种子菜单失败: %v", err)
	}

	code := "menu_tree:" + uniq("")
	createPerm(t, e, code, "/api/tree")

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "顶级目录", Type: adminmodel.MenuTypeDirectory, Path: "/top", Status: 1,
	}); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	var dir adminmodel.MenuEntity
	if err := e.db.Where("title = ?", "顶级目录").First(&dir).Error; err != nil {
		t.Fatalf("查询目录失败: %v", err)
	}

	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "子菜单X", Type: adminmodel.MenuTypeMenu, Path: "/sub", PermissionCodes: []string{code}, ParentID: dir.ID, Status: 1,
	}); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}

	// 孤儿行由历史数据造成，不是可创建的状态：MenuCreate 现在会锁父行，
	// 父不存在即 ErrMenuParentNotFound —— 这条生产校验不能为了造测试数据而削弱。
	// 树构建的容错必须仍然成立（父缺失的节点要升为根，不能被丢掉），所以这里
	// 直接写一条「父已不存在」的脏记录来复现历史现场。
	orphan := &adminmodel.MenuEntity{
		Title: "孤儿菜单", Type: adminmodel.MenuTypeMenu, Path: "/orphan", ParentID: 987654, Status: 1,
	}
	if err := e.db.WithContext(ctx).Create(orphan).Error; err != nil {
		t.Fatalf("写入历史孤儿菜单失败: %v", err)
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

func TestMenuSoftDelete(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "menu_soft:" + uniq("")
	createPerm(t, e, code, "/api/soft")
	if err := e.svc.MenuCreate(ctx, &admindto.MenuCreateReq{
		Title: "待软删", Type: adminmodel.MenuTypeMenu, Path: "/soft", PermissionCodes: []string{code}, Status: 1,
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
	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ? AND deleted_at IS NULL", menu.ID).Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("软删除后应不可见")
	}

	var rawCount int64
	if err := e.db.Model(&adminmodel.MenuEntity{}).Where("id = ?", menu.ID).Count(&rawCount).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if rawCount != 1 {
		t.Fatalf("软删除应保留原始行")
	}
}
