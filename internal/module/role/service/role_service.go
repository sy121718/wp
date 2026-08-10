// Package roleservice 角色模块业务逻辑实现。
package roleservice

import (
	admincontract "go_wp/internal/module/admin/contract"
	rolecontract "go_wp/internal/module/role/contract"
	menucontract "go_wp/internal/module/menu/contract"
	permissioncontract "go_wp/internal/module/permission/contract"
	rolemodel "go_wp/internal/module/role/model"
)

var _ rolecontract.RoleService = (*Service)(nil)

// Service 角色业务逻辑。
//
// 依赖链：
//   - rolemodel：只碰 sys_role
//   - menuSvc：menu_ids → permission_codes
//   - permSvc：permission_codes → [path, method, code]
//   - adminSvc：user_ids → 用户详情（通过 SetAdminService 注入，避免循环依赖）
//   - casbin：p/g/g2 策略写入
type Service struct {
	rm      *rolemodel.RoleModel
	menuSvc menucontract.MenuService
	permSvc permissioncontract.PermissionService
	adminSvc admincontract.AdminService
}

// NewService 创建角色 Service 实例，注入 model 与跨模块契约。
func NewService(
	rm *rolemodel.RoleModel,
	menuSvc menucontract.MenuService,
	permSvc permissioncontract.PermissionService,
) *Service {
	return &Service{rm: rm, menuSvc: menuSvc, permSvc: permSvc}
}

// SetAdminService 注入 adminService。
//
// 用于在 routes 层打破循环依赖：
//   role → admin（查用户详情），admin → role（角色绑定）
// 不能在构造函数中同时注入，因此通过 setter 在装配完成后设置。
func (s *Service) SetAdminService(svc admincontract.AdminService) {
	s.adminSvc = svc
}