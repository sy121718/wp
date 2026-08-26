// Package admincontract 定义 admin 模块（管理员/角色/权限点/菜单/部门/数据权限）
// 对 http 层暴露的业务契约接口。
//
// 接口按领域拆分（窄接口），由合并后的单一 Service 实现；
// handle 层只依赖契约，便于 mock 测试与未来多实现替换。
package admincontract

import (
	"context"

	admindto "go_wp/internal/module/admin/dto"
)

// AdminService 管理员领域业务能力。
type AdminService interface {
	AdminList(ctx context.Context, req *admindto.AdminListReq) (*admindto.AdminListResp, error)
	AdminLogin(ctx context.Context, req *admindto.AdminLoginReq, clientIP string) (*admindto.AdminLoginResp, error)
	AdminLogout(ctx context.Context, userID uint64) error
	AdminProfile(ctx context.Context, userID uint64) (*admindto.AdminProfileResp, error)
	AdminCreate(ctx context.Context, req *admindto.AdminCreateReq) (*admindto.AdminCreateResp, error)
	AdminEdit(ctx context.Context, req *admindto.AdminEditReq) (*admindto.AdminEditResp, error)
	AdminDetail(ctx context.Context, req *admindto.AdminDetailReq) (*admindto.AdminDetailResp, error)
	AdminDelete(ctx context.Context, req *admindto.AdminDeleteReq) (*admindto.AdminDeleteResp, error)
	AdminRoleList(ctx context.Context, req *admindto.AdminRoleListReq) (*admindto.AdminRoleListResp, error)
	AdminRoleSave(ctx context.Context, req *admindto.AdminRoleSaveReq) (*admindto.AdminRoleSaveResp, error)
	AdminMenuList(ctx context.Context, req *admindto.AdminMenuListReq) (*admindto.AdminMenuListResp, error)
	AdminMenuSave(ctx context.Context, req *admindto.AdminMenuSaveReq) (*admindto.AdminMenuSaveResp, error)
	AdminRoutes(ctx context.Context, userID uint64, lang string) (*admindto.AdminRoutesResp, error)
}

// RoleService 角色领域业务能力。
type RoleService interface {
	RoleList(ctx context.Context, req *admindto.RoleListReq) (*admindto.RoleListResp, error)
	RoleDetail(ctx context.Context, req *admindto.RoleDetailReq) (*admindto.RoleDetailResp, error)
	RoleCreate(ctx context.Context, req *admindto.RoleCreateReq) error
	RoleUpdate(ctx context.Context, req *admindto.RoleUpdateReq) error
	RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error
	RoleMenuList(ctx context.Context, req *admindto.RoleMenuListReq) (*admindto.RoleMenuListResp, error)
	RoleMenuSave(ctx context.Context, req *admindto.RoleMenuSaveReq) (*admindto.RoleMenuSaveResp, error)
	RoleUserList(ctx context.Context, req *admindto.RoleUserListReq) (*admindto.RoleUserListResp, error)
	RoleUserSave(ctx context.Context, req *admindto.RoleUserSaveReq) (*admindto.RoleUserSaveResp, error)
}

// PermService 权限点领域业务能力。
type PermService interface {
	PermList(ctx context.Context, req *admindto.PermListReq) (*admindto.PermListResp, error)
	PermDetail(ctx context.Context, req *admindto.PermDetailReq) (*admindto.PermDetailResp, error)
	PermOptions(ctx context.Context, req *admindto.PermOptionsReq) (*admindto.PermOptionsResp, error)
	PermCreate(ctx context.Context, req *admindto.PermCreateReq) (*admindto.PermCreateResp, error)
	PermUpdate(ctx context.Context, req *admindto.PermUpdateReq) (*admindto.PermUpdateResp, error)
	PermDelete(ctx context.Context, req *admindto.PermDeleteReq) (*admindto.PermDeleteResp, error)
}

// MenuService 菜单领域业务能力。
type MenuService interface {
	MenuTree(ctx context.Context, req *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error)
	MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (*admindto.MenuDetailResp, error)
	MenuCreate(ctx context.Context, req *admindto.MenuCreateReq) error
	MenuUpdate(ctx context.Context, req *admindto.MenuUpdateReq) error
	MenuDelete(ctx context.Context, req *admindto.MenuDeleteReq) error
}

// DeptService 部门领域业务能力。
type DeptService interface {
	DeptTree(ctx context.Context) ([]admindto.DeptTreeNode, error)
	DeptDetail(ctx context.Context, req *admindto.DeptDetailReq) (*admindto.DeptTreeNode, error)
	DeptCreate(ctx context.Context, req *admindto.DeptCreateReq) error
	DeptUpdate(ctx context.Context, req *admindto.DeptUpdateReq) error
	DeptDelete(ctx context.Context, req *admindto.DeptDeleteReq) error
	DeptUserList(ctx context.Context, req *admindto.DeptUserListReq) (*admindto.AdminListByDeptIDResp, error)
	DeptUserSave(ctx context.Context, req *admindto.DeptUserSaveReq) error
}

// RuleService 数据权限规则领域业务能力。
type RuleService interface {
	RuleList(ctx context.Context, req *admindto.RuleListReq) (*admindto.RuleListResp, error)
	RuleDetail(ctx context.Context, req *admindto.RuleDetailReq) (*admindto.RuleDetailResp, error)
	RuleCreate(ctx context.Context, req *admindto.RuleCreateReq) error
	RuleUpdate(ctx context.Context, req *admindto.RuleUpdateReq) error
	RuleDelete(ctx context.Context, req *admindto.RuleDeleteReq) error
	RuleSchemaList(ctx context.Context) ([]admindto.RuleDomainItem, error)
	RuleSchemaDetail(ctx context.Context, req *admindto.RuleSchemaDetailReq) (*admindto.RuleDomainDetail, error)
	RuleAssignmentList(ctx context.Context, req *admindto.RuleAssignmentListReq) (*admindto.RuleAssignmentListResp, error)
	RuleAssignmentSave(ctx context.Context, req *admindto.RuleAssignmentSaveReq) error
}