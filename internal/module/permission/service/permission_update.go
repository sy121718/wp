package permissionservice

import (
	"context"
	"errors"
	"fmt"

	permissiondto "go_wp/internal/module/permission/dto"
	permissionenums "go_wp/internal/module/permission/enums"
	permissionmodel "go_wp/internal/module/permission/model"
	"go_wp/pkg/casbin"
)

// Update 更新权限点定义，并同步已分配的 Casbin 策略。
func (s *Service) Update(ctx context.Context, req *permissiondto.UpdateReq) (res *permissiondto.UpdateResp, err error) {
	entity, err := s.pm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(permissionenums.ErrPermissionNotFound)
	}

	if req.PermissionCode != entity.PermissionCode {
		return nil, errors.New(permissionenums.ErrCodeImmutable)
	}

	assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
	if err != nil {
		return nil, err
	}
	if req.Status == permissionmodel.PermissionStatusDisabled && assigned {
		return nil, errors.New(permissionenums.ErrPermissionAssigned)
	}

	oldEntity := *entity
	definitionChanged := req.APIPath != entity.APIPath || req.APIMethod != entity.APIMethod
	entity.PermissionName = req.PermissionName
	entity.Module = req.Module
	entity.APIPath = req.APIPath
	entity.APIMethod = req.APIMethod
	entity.Status = req.Status
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	if err = s.pm.Update(ctx, entity); err != nil {
		return nil, err
	}
	if definitionChanged && assigned {
		if err = casbin.ReplacePermissionDefinition(entity.PermissionCode, entity.APIPath, entity.APIMethod); err != nil {
			if rollbackErr := s.pm.Update(ctx, &oldEntity); rollbackErr != nil {
				return nil, fmt.Errorf("同步 Casbin 策略失败: %v；恢复权限点失败: %w", err, rollbackErr)
			}
			return nil, err
		}
	}

	return &permissiondto.UpdateResp{ID: req.ID}, nil
}
