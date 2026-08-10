package adminservice

import (
	admincontract "go_wp/internal/module/admin/contract"
	adminmodel "go_wp/internal/module/admin/model"
	menucontract "go_wp/internal/module/menu/contract"
	permissioncontract "go_wp/internal/module/permission/contract"
	rolecontract "go_wp/internal/module/role/contract"
)

var _ admincontract.AdminService = (*Service)(nil)

// Service 定义了 Admin 模块的业务逻辑。
//
// 依赖链：
//   - am：只碰 sys_admin
//   - menuSvc：构建授权菜单树
//   - roleSvc：查用户的角色编码
//   - permSvc：permission_code → path/method
type Service struct {
	am      *adminmodel.AdminModel
	menuSvc menucontract.MenuService
	roleSvc rolecontract.RoleService
	permSvc permissioncontract.PermissionService
}

func NewService(
	am *adminmodel.AdminModel,
	menuSvc menucontract.MenuService,
	roleSvc rolecontract.RoleService,
	permSvc permissioncontract.PermissionService,
) *Service {
	return &Service{
		am:      am,
		menuSvc: menuSvc,
		roleSvc: roleSvc,
		permSvc: permSvc,
	}
}