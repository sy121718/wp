package permissionservice

import (
	menucontract "go_wp/internal/module/menu/contract"
	permissioncontract "go_wp/internal/module/permission/contract"
	permissionmodel "go_wp/internal/module/permission/model"
)

var _ permissioncontract.PermissionService = (*Service)(nil)

type Service struct {
	pm      *permissionmodel.PermissionModel
	menuSvc menucontract.MenuService
}

func NewService(pm *permissionmodel.PermissionModel) *Service {
	return &Service{pm: pm}
}

// SetMenuService 后置注入菜单契约，用于删除权限前检查引用。
func (s *Service) SetMenuService(menuSvc menucontract.MenuService) {
	s.menuSvc = menuSvc
}
