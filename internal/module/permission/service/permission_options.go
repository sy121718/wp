package permissionservice

import (
	"context"

	permissiondto "go_wp/internal/module/permission/dto"
	permissionmodel "go_wp/internal/module/permission/model"
)

// Options 返回启用权限选项（供菜单表单选择 permission_code）。
func (s *Service) Options(ctx context.Context, req *permissiondto.OptionsReq) (res *permissiondto.OptionsResp, err error) {
	var entities []permissionmodel.PermissionEntity
	query := s.pm.DB(ctx).Where("status = ?", permissionmodel.PermissionStatusEnabled)
	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	err = query.Order("module ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, err
	}

	list := make([]permissiondto.OptionItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, permissiondto.OptionItem{
			ID:             e.ID,
			PermissionCode: e.PermissionCode,
			PermissionName: e.PermissionName,
			Module:         e.Module,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
		})
	}
	return &permissiondto.OptionsResp{List: list}, nil
}

// ListByCodes 按 permission_code 列表批量查权限摘要。
// 供 menu/role/admin 把 code 转换为 path/method。
func (s *Service) ListByCodes(ctx context.Context, codes []string) ([]permissiondto.PermissionBrief, error) {
	entities, err := s.pm.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	result := make([]permissiondto.PermissionBrief, 0, len(entities))
	for _, e := range entities {
		result = append(result, permissiondto.PermissionBrief{
			PermissionCode: e.PermissionCode,
			APIPath:        e.APIPath,
			APIMethod:      e.APIMethod,
		})
	}
	return result, nil
}

// ExistsEnabledCode 检查 code 是否存在且启用。
func (s *Service) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	return s.pm.ExistsEnabledCode(ctx, code)
}