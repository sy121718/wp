package unit

import (
	"context"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	pkgcasbin "go_wp/pkg/casbin"
)

func TestRoleCreateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_" + uniq("")
	enabled := adminmodel.RoleStatusEnabled
	err := e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{
		RoleCode: code, RoleName: "测试角色", Status: &enabled, SortOrder: 2,
	})
	wantErr(t, err, "")

	var role adminmodel.RoleEntity
	if err := e.db.Where("role_code = ?", code).First(&role).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if role.RoleName != "测试角色" || role.SortOrder != 2 || role.Status != adminmodel.RoleStatusEnabled {
		t.Fatalf("角色字段不符: %+v", role)
	}

	ok, err := pkgcasbin.GetEnforcer().HasNamedGroupingPolicy("g2", code, "active")
	wantErr(t, err, "")
	if !ok {
		t.Fatalf("新建启用角色应写入 g2 active")
	}
}

func TestRoleCreateStatusZeroDefaultsEnabled(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_zero_" + uniq("")
	err := e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{RoleCode: code, RoleName: "零值角色"})
	wantErr(t, err, "")

	var role adminmodel.RoleEntity
	if err := e.db.Where("role_code = ?", code).First(&role).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if role.Status != adminmodel.RoleStatusEnabled {
		t.Fatalf("status 未传应默认启用: got=%d", role.Status)
	}
}

func TestRoleCreateDisabledExplicit(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_disabled_" + uniq("")
	disabled := adminmodel.RoleStatusDisabled
	err := e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{
		RoleCode: code, RoleName: "禁用角色", Status: &disabled,
	})
	wantErr(t, err, "")

	var role adminmodel.RoleEntity
	if err := e.db.Where("role_code = ?", code).First(&role).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if role.Status != adminmodel.RoleStatusDisabled {
		t.Fatalf("显式禁用应落库 status=0: got=%d", role.Status)
	}

	ok, err := pkgcasbin.GetEnforcer().HasNamedGroupingPolicy("g2", code, "active")
	wantErr(t, err, "")
	if ok {
		t.Fatalf("新建禁用角色不应写入 g2 active")
	}
}

func TestRoleCreateNumericCodeRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{RoleCode: "123456", RoleName: "数字角色"})
	wantErr(t, err, adminenums.ErrRoleCodeNumeric)
}

func TestRoleCreateDuplicateCode(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_dup_" + uniq("")
	err := e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{RoleCode: code, RoleName: "角色A"})
	wantErr(t, err, "")
	err = e.svc.RoleCreate(ctx, &admindto.RoleCreateReq{RoleCode: code, RoleName: "角色B"})
	wantErr(t, err, adminenums.ErrRoleCodeExists)
}

func TestRoleUpdateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_upd_" + uniq("")
	roleID := createRole(t, e, code, "旧名称")
	remark := "新备注"
	err := e.svc.RoleUpdate(ctx, &admindto.RoleUpdateReq{
		ID: roleID, RoleName: "新名称", Status: 1, SortOrder: 9, Remark: remark,
	})
	wantErr(t, err, "")

	var role adminmodel.RoleEntity
	if err := e.db.First(&role, roleID).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if role.RoleName != "新名称" || role.SortOrder != 9 || role.Remark == nil || *role.Remark != remark {
		t.Fatalf("角色更新不符: %+v", role)
	}
}

func TestRoleUpdateRefreshesUpdateTime(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_ref_" + uniq("")
	roleID := createRole(t, e, code, "刷新时间角色")

	past := time.Now().Add(-time.Hour)
	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("id = ?", roleID).
		Updates(map[string]interface{}{"update_time": past, "update_by": 10086}).Error; err != nil {
		t.Fatalf("改写审计字段失败: %v", err)
	}

	err := e.svc.RoleUpdate(ctx, &admindto.RoleUpdateReq{
		ID: roleID, RoleName: "刷新时间后", Status: 1,
	})
	wantErr(t, err, "")

	var role adminmodel.RoleEntity
	if err := e.db.First(&role, roleID).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	if role.UpdateTime == nil {
		t.Fatalf("更新后 update_time 不应为 nil")
	}
	if !role.UpdateTime.After(past) {
		t.Fatalf("更新后 update_time 应被刷新为当前时间: got=%v want after %v", role.UpdateTime, past)
	}
	if role.UpdateBy != 10086 {
		t.Fatalf("update_by 应保留原值: got=%d want=10086", role.UpdateBy)
	}
}

func TestRoleUpdateDisableSyncsCasbin(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_dis_" + uniq("")
	roleID := createRole(t, e, code, "将被禁用")
	err := e.svc.RoleUpdate(ctx, &admindto.RoleUpdateReq{
		ID: roleID, RoleName: "将被禁用", Status: adminmodel.RoleStatusDisabled,
	})
	wantErr(t, err, "")

	ok, err := pkgcasbin.GetEnforcer().HasNamedGroupingPolicy("g2", code, "active")
	wantErr(t, err, "")
	if ok {
		t.Fatalf("禁用角色后 g2 active 应被删除")
	}
}

func TestRoleUpdateNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RoleUpdate(ctx, &admindto.RoleUpdateReq{ID: 999999, RoleName: "x", Status: 1})
	wantErr(t, err, adminenums.ErrRoleNotFound)
}

func TestRoleDeleteSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_del_" + uniq("")
	roleID := createRole(t, e, code, "待删除")

	if err := pkgcasbin.ReplaceUserRoleBindings("1001", []string{code}); err != nil {
		t.Fatalf("绑定用户失败: %v", err)
	}
	if err := pkgcasbin.ReplaceRolePermissions(code, [][3]string{{"/api/r", "GET", "r:list"}}); err != nil {
		t.Fatalf("写入角色权限失败: %v", err)
	}

	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: roleID})
	wantErr(t, err, "")

	var count int64
	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("id = ?", roleID).Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("角色记录应被删除")
	}
	perms, err := pkgcasbin.GetRolePermissions(code)
	wantErr(t, err, "")
	if len(perms) != 0 {
		t.Fatalf("角色权限策略应被清理: %v", perms)
	}
	users, err := pkgcasbin.GetUserIDsByRoleCode(code)
	wantErr(t, err, "")
	if len(users) != 0 {
		t.Fatalf("角色用户绑定应被清理: %v", users)
	}
}

func TestRoleDeleteSystemRoleRejected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_sys_" + uniq("")
	roleID := createRole(t, e, code, "系统角色")
	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("id = ?", roleID).Update("is_system", 1).Error; err != nil {
		t.Fatalf("设置系统角色失败: %v", err)
	}
	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: roleID})
	wantErr(t, err, adminenums.ErrRoleIsSystem)
}

func TestRoleDeleteNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	err := e.svc.RoleDelete(ctx, &admindto.RoleDeleteReq{ID: 888888})
	wantErr(t, err, adminenums.ErrRoleNotFound)
}

func TestRoleDetailSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "role_det_" + uniq("")
	roleID := createRole(t, e, code, "详情角色")
	res, err := e.svc.RoleDetail(ctx, &admindto.RoleDetailReq{ID: roleID})
	wantErr(t, err, "")
	if res.RoleCode != code || res.RoleName != "详情角色" {
		t.Fatalf("详情不符: %+v", res)
	}
}

func TestRoleDetailNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.RoleDetail(ctx, &admindto.RoleDetailReq{ID: 777777})
	wantErr(t, err, adminenums.ErrRoleNotFound)
}

func TestRoleListPagingAndKeyword(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	kw := "keyword_" + uniq("")
	createRole(t, e, "role_k1_"+uniq(""), kw+"A")
	createRole(t, e, "role_k2_"+uniq(""), kw+"B")
	createRole(t, e, "role_other_"+uniq(""), "无关角色")

	res, err := e.svc.RoleList(ctx, &admindto.RoleListReq{Keyword: kw})
	wantErr(t, err, "")
	if res.Total != 2 {
		t.Fatalf("关键字筛选总数不符: got=%d", res.Total)
	}

	res, err = e.svc.RoleList(ctx, &admindto.RoleListReq{})
	wantErr(t, err, "")
	if res.Total != 3 {
		t.Fatalf("全量总数不符: got=%d", res.Total)
	}
	if len(res.List) > 3 {
		t.Fatalf("默认分页返回过多: %d", len(res.List))
	}
}

func TestRoleMenuSaveSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "rolemenu_" + uniq("")
	roleID := createRole(t, e, "rm_"+uniq(""), "菜单角色")
	permID := createPerm(t, e, code, "/api/rolemenu")

	createPerm(t, e, "bg_"+uniq(""), "/api/background")
	menuID := createMenuWithPerm(t, e, permID, code)

	res, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{
		RoleID: roleID, MenuIDs: []uint64{menuID},
	})
	wantErr(t, err, "")
	if res.RoleID != roleID {
		t.Fatalf("响应 RoleID 不符")
	}

	role, err := e.dbGetRole(roleID)
	wantErr(t, err, "")
	perms, err := pkgcasbin.GetRolePermissions(role.RoleCode)
	wantErr(t, err, "")
	if len(perms) != 1 || perms[0][0] != "/api/rolemenu" || perms[0][1] != "GET" || perms[0][2] != code {
		t.Fatalf("角色权限策略不符: %v", perms)
	}

	list, err := e.svc.RoleMenuList(ctx, &admindto.RoleMenuListReq{RoleID: roleID})
	wantErr(t, err, "")
	if len(list.MenuIDs) != 1 || list.MenuIDs[0] != menuID {
		t.Fatalf("RoleMenuList 反查不符: %v", list.MenuIDs)
	}
}

func TestRoleMenuSaveRoleNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.RoleMenuSave(ctx, &admindto.RoleMenuSaveReq{RoleID: 999999})
	wantErr(t, err, adminenums.ErrRoleNotFound)
}

func TestRoleUserSaveAndList(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	code := "ru_" + uniq("")
	roleID := createRole(t, e, code, "用户角色")
	u1 := createAdminDB(t, e.db, uniq("ru1"), uniq("ru1")+"@example.com")
	u2 := createAdminDB(t, e.db, uniq("ru2"), uniq("ru2")+"@example.com")

	_, err := e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{
		RoleID: roleID, UserIDs: []uint64{u1, u2},
	})
	wantErr(t, err, "")

	role, err := e.dbGetRole(roleID)
	wantErr(t, err, "")
	userIDs, err := pkgcasbin.GetUserIDsByRoleCode(role.RoleCode)
	wantErr(t, err, "")
	if len(userIDs) != 2 {
		t.Fatalf("角色用户绑定数不符: %v", userIDs)
	}

	list, err := e.svc.RoleUserList(ctx, &admindto.RoleUserListReq{RoleID: roleID})
	wantErr(t, err, "")
	if list.Total != 2 || len(list.List) != 2 {
		t.Fatalf("RoleUserList 不符: total=%d len=%d", list.Total, len(list.List))
	}
	for _, item := range list.List {
		if item.Username == "" {
			t.Fatalf("角色用户应带出用户名")
		}
	}
}

func TestRoleUserSaveNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.RoleUserSave(ctx, &admindto.RoleUserSaveReq{RoleID: 666666, UserIDs: []uint64{1}})
	wantErr(t, err, adminenums.ErrRoleNotFound)
}

func TestGetRoleCodesByUserIDFiltersDisabled(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	enabledCode := "gc_en_1_" + uniq("")
	disabledCode := "gc_dis_1_" + uniq("")
	createRole(t, e, enabledCode, "启用角色")
	disID := createRole(t, e, disabledCode, "禁用角色")

	if err := e.db.Model(&adminmodel.RoleEntity{}).Where("id = ?", disID).Update("status", adminmodel.RoleStatusDisabled).Error; err != nil {
		t.Fatalf("禁用角色失败: %v", err)
	}

	userID := "10086"
	if err := pkgcasbin.ReplaceUserRoleBindings(userID, []string{enabledCode, disabledCode}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}

	codes, err := e.svc.GetRoleCodesByUserID(ctx, 10086)
	wantErr(t, err, "")
	if len(codes) != 1 || codes[0] != enabledCode {
		t.Fatalf("有效角色应仅含启用角色: %v", codes)
	}
}

func (e *env) dbGetRole(id uint64) (*adminmodel.RoleEntity, error) {
	var role adminmodel.RoleEntity
	err := e.db.First(&role, id).Error
	return &role, err
}

func createMenuWithPerm(t *testing.T, e *env, _ uint64, code string) uint64 {
	t.Helper()
	err := e.svc.MenuCreate(context.Background(), &admindto.MenuCreateReq{
		Title:           "测试菜单 " + uniq(""),
		Type:            adminmodel.MenuTypeMenu,
		Path:            "/test/menu",
		PermissionCodes: []string{code},
		Status:          1,
	})
	wantErr(t, err, "")
	var menu adminmodel.MenuEntity
	if err := e.db.Where("title LIKE ?", "测试菜单%").Order("id DESC").First(&menu).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	return menu.ID
}
