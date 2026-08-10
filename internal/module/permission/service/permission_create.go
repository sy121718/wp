package permissionservice

import (
	"context"
	"errors"

	permissiondto "go_wp/internal/module/permission/dto"
	permissionenums "go_wp/internal/module/permission/enums"
	permissionmodel "go_wp/internal/module/permission/model"
)

// Create 新建权限点。
func (s *Service) Create(ctx context.Context, req *permissiondto.CreateReq) (res *permissiondto.CreateResp, err error) {
	// 检查 code 唯一性
	existing, err := s.pm.GetByCode(ctx, req.PermissionCode)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New(permissionenums.ErrCodeExists)
	}

	entity := &permissionmodel.PermissionEntity{
		PermissionCode: req.PermissionCode,
		PermissionName: req.PermissionName,
		Module:         req.Module,
		APIPath:        req.APIPath,
		APIMethod:      req.APIMethod,
		Status:         req.Status,
	}
	if entity.Status == 0 {
		entity.Status = permissionmodel.PermissionStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err = s.pm.Create(ctx, entity); err != nil {
		return nil, err
	}

	return &permissiondto.CreateResp{ID: entity.ID}, nil
}