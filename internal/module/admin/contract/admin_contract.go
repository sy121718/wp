package admincontract

import (
	"context"

	admindto "go_wp/internal/module/admin/dto"
)

type AdminService interface {
	AdminList(ctx context.Context, req *admindto.AdminListReq) (*admindto.AdminListResp, error)
	AdminLogin(ctx context.Context, req *admindto.AdminLoginReq, clientIP string) (*admindto.AdminLoginResp, error)

	DevLogin(ctx context.Context, username string) (*admindto.AdminLoginResp, error)
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
}

type RoleService interface {
	RoleList(ctx context.Context, req *admindto.RoleListReq) (*admindto.RoleListResp, error)
	RoleDetail(ctx context.Context, req *admindto.RoleDetailReq) (*admindto.RoleDetailResp, error)
	RoleCreate(ctx context.Context, req *admindto.RoleCreateReq) error
	RoleUpdate(ctx context.Context, req *admindto.RoleUpdateReq) error
	RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error
	RoleMenuList(ctx context.Context, req *admindto.RoleMenuListReq) (*admindto.RoleMenuListResp, error)

	RolePermissionTree(ctx context.Context, roleID uint64) (*admindto.RolePermissionTreeResp, error)
	RoleMenuSave(ctx context.Context, req *admindto.RoleMenuSaveReq) (*admindto.RoleMenuSaveResp, error)
	RoleUserList(ctx context.Context, req *admindto.RoleUserListReq) (*admindto.RoleUserListResp, error)
	RoleUserSave(ctx context.Context, req *admindto.RoleUserSaveReq) (*admindto.RoleUserSaveResp, error)
}

type PermService interface {
	PermList(ctx context.Context, req *admindto.PermListReq) (*admindto.PermListResp, error)
	PermDetail(ctx context.Context, req *admindto.PermDetailReq) (*admindto.PermDetailResp, error)
	PermOptions(ctx context.Context, req *admindto.PermOptionsReq) (*admindto.PermOptionsResp, error)
	PermCreate(ctx context.Context, req *admindto.PermCreateReq) (*admindto.PermCreateResp, error)
	PermUpdate(ctx context.Context, req *admindto.PermUpdateReq) (*admindto.PermUpdateResp, error)
	PermDelete(ctx context.Context, req *admindto.PermDeleteReq) (*admindto.PermDeleteResp, error)
}

type MenuService interface {
	MenuPage(ctx context.Context, page, limit int, keyword string) (*admindto.MenuPageResp, error)

	MenuParentOptions(ctx context.Context, excludeID uint64) ([]admindto.MenuParentChoice, error)
	MenuTree(ctx context.Context, req *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error)
	MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (*admindto.MenuDetailResp, error)
	MenuCreate(ctx context.Context, req *admindto.MenuCreateReq) error
	MenuUpdate(ctx context.Context, req *admindto.MenuUpdateReq) error
	MenuDelete(ctx context.Context, req *admindto.MenuDeleteReq) error
}

type DeptService interface {
	DeptPage(ctx context.Context, page, limit int, keyword string) (*admindto.DeptPageResp, error)
	DeptTree(ctx context.Context) ([]admindto.DeptTreeNode, error)
	DeptDetail(ctx context.Context, req *admindto.DeptDetailReq) (*admindto.DeptTreeNode, error)
	DeptCreate(ctx context.Context, req *admindto.DeptCreateReq) error
	DeptUpdate(ctx context.Context, req *admindto.DeptUpdateReq) error
	DeptDelete(ctx context.Context, req *admindto.DeptDeleteReq) error
	DeptUserList(ctx context.Context, req *admindto.DeptUserListReq) (*admindto.AdminListByDeptIDResp, error)
	DeptUserSave(ctx context.Context, req *admindto.DeptUserSaveReq) error
}

type AuthzContextService interface {
	IsSuperAdmin(ctx context.Context, userID uint64) (bool, error)

	GetRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error)

	GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error)

	ListByCodes(ctx context.Context, codes []string) ([]admindto.PermBrief, error)

	ExistsEnabledCode(ctx context.Context, code string) (bool, error)

	EffectivePermissionCodes(ctx context.Context, userID uint64) ([]string, error)

	AuthorizedButtonCodes(ctx context.Context, codes []string) ([]string, error)

	BuildAuthorizedTree(ctx context.Context, codes []string) ([]admindto.MenuTreeNode, error)
}

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
