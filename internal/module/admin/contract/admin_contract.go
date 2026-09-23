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
	// DevLogin 开发阶段免密登录（仅 debug 模式的路由会调用它）。
	//
	// 放在契约里不是「对外开放」的意思：调用方只有装配层（routers）在 debug 模式下挂的那一个页面路由。
	// 它只登超管，且会话建立与 AdminLogin 完全共用同一条路径。
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
	// RolePermissionTree 返回角色权限分配树与该角色当前勾选（页面与 JSON 接口共用）。
	RolePermissionTree(ctx context.Context, roleID uint64) (*admindto.RolePermissionTreeResp, error)
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
	MenuPage(ctx context.Context, page, limit int, keyword string) (*admindto.MenuPageResp, error)
	MenuTree(ctx context.Context, req *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error)
	MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (*admindto.MenuDetailResp, error)
	MenuCreate(ctx context.Context, req *admindto.MenuCreateReq) error
	MenuUpdate(ctx context.Context, req *admindto.MenuUpdateReq) error
	MenuDelete(ctx context.Context, req *admindto.MenuDeleteReq) error
}

// DeptService 部门领域业务能力。
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

// AuthzContextService 面向外部模块/插件的权限上下文查询能力。
//
// 与 AdminService/RoleService 等管理面 CRUD 接口不同，本接口只暴露「某用户在
// 其角色/权限/超管范围内的只读上下文」，供外部模块（如 dashboard、后续插件体系）
// 消费，而无需导入 admin 的 model/service。管理面写操作仍由 handle 层经
// AdminService 等走 Casbin 鉴权，不在此暴露。
type AuthzContextService interface {
	// IsSuperAdmin 是否为超管（is_admin=1）。
	IsSuperAdmin(ctx context.Context, userID uint64) (bool, error)
	// GetRoleCodesByUserID 查询用户绑定且已启用的角色编码列表。
	GetRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error)
	// GetPermissionCodesByIDs 根据 menu_id 列表收集 type=2/3 的 permission_code 并去重。
	GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error)
	// ListByCodes 按 permission code 列表查询权限点概要（path/method/code）。
	ListByCodes(ctx context.Context, codes []string) ([]admindto.PermBrief, error)
	// ExistsEnabledCode 指定权限点编码是否存在且启用。
	ExistsEnabledCode(ctx context.Context, code string) (bool, error)
	// BuildAuthorizedRoutes 根据权限 codes 构建当前用户可见路由树。
	BuildAuthorizedRoutes(ctx context.Context, codes []string, lang string) ([]admindto.RouteNode, error)
	// EffectivePermissionCodes 用户全部有效权限码（直接 + 角色继承）。
	// 渲染层据此做菜单 / 按钮 / 字段可见性过滤，与 Casbin API 鉴权同源。
	EffectivePermissionCodes(ctx context.Context, userID uint64) ([]string, error)
	// BuildAuthorizedTree 根据权限 codes 构建后台导航树：只含目录（type=1）与菜单（type=2），
	// 自动补齐可见子项的祖先目录，排除按钮 / iframe / 外链；is_public=1 的空权限项照常可见。
	// 后台侧栏直接消费它 —— 菜单真源是 sys_menus 表，各页面不再有 Go 侧硬编码菜单表。
	// TitleKey / Title 原样透出，标题翻译由渲染层决定（本模块不依赖模板层文案）。
	BuildAuthorizedTree(ctx context.Context, codes []string) ([]admindto.MenuTreeNode, error)
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
