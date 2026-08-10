// Package menuservice 菜单模块业务逻辑实现。
package menuservice

import (
	menucontract "go_wp/internal/module/menu/contract"
	menumodel "go_wp/internal/module/menu/model"
	permissioncontract "go_wp/internal/module/permission/contract"
)

var _ menucontract.MenuService = (*Service)(nil)

// Service 菜单业务逻辑。
// 依赖 permission contract 用于校验 permission_code 是否启用。
type Service struct {
	mm       *menumodel.MenuModel
	permSvc  permissioncontract.PermissionService
}

func NewService(mm *menumodel.MenuModel, permSvc permissioncontract.PermissionService) *Service {
	return &Service{mm: mm, permSvc: permSvc}
}