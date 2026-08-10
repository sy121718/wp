package adminservice

import (
	"context"
	"errors"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
)

// Delete 删除普通管理员，并撤销其会话和授权。
func (s *Service) Delete(ctx context.Context, req *admindto.DeleteReq) (res *admindto.DeleteResp, err error) {
	ids := uniqueAdminIDs(req.Id)
	if len(ids) == 0 {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	for _, id := range ids {
		entity, err := s.am.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if entity == nil {
			return nil, errors.New(adminenums.ErrAdminNotFound)
		}
		if id == req.OperatorID {
			return nil, errors.New(adminenums.ErrDeleteSelf)
		}
		if entity.IsSuperAdmin() {
			return nil, errors.New(adminenums.ErrDeleteSuperAdmin)
		}
	}

	// 先撤销旧 JWT，确保后续任一步骤失败时都不会继续放行已删除账号。
	for _, id := range ids {
		if err = auth.RevokeUserSession(ctx, id); err != nil {
			return nil, err
		}
	}

	deleted, err := s.am.DeleteByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if deleted != int64(len(ids)) {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	for _, id := range ids {
		if err = casbin.DeleteUserAllPolicies(strconv.FormatUint(id, 10)); err != nil {
			return nil, err
		}
	}
	return &admindto.DeleteResp{DeletedCount: deleted}, nil
}

func uniqueAdminIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	result := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
