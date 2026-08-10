package permissionservice

import (
	"context"
	"errors"

	permissiondto "go_wp/internal/module/permission/dto"
	permissionenums "go_wp/internal/module/permission/enums"
	"go_wp/pkg/casbin"
)

// Delete 批量删除未分配的权限点。
func (s *Service) Delete(ctx context.Context, req *permissiondto.DeleteReq) (res *permissiondto.DeleteResp, err error) {
	entities, err := s.pm.ListByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	if len(entities) != len(req.IDs) {
		return nil, errors.New(permissionenums.ErrPermissionNotFound)
	}
	codes := make([]string, 0, len(entities))
	for _, entity := range entities {
		assigned, err := casbin.HasPermissionPolicies(entity.PermissionCode)
		if err != nil {
			return nil, err
		}
		if assigned {
			return nil, errors.New(permissionenums.ErrPermissionAssigned)
		}
		codes = append(codes, entity.PermissionCode)
	}

	if s.menuSvc == nil {
		return nil, errors.New(permissionenums.ErrMenuCheckerUnavailable)
	}
	references, err := s.menuSvc.CountByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	if references > 0 {
		return nil, errors.New(permissionenums.ErrMenuReferenced)
	}

	deleted, err := s.pm.DeleteByIDs(ctx, req.IDs)
	if err != nil {
		return nil, err
	}
	return &permissiondto.DeleteResp{DeletedCount: deleted}, nil
}
