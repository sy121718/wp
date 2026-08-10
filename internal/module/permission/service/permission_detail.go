package permissionservice

import (
	"context"
	"errors"

	permissiondto "go_wp/internal/module/permission/dto"
	permissionenums "go_wp/internal/module/permission/enums"

	"gorm.io/gorm"
)

// Detail 查询单个权限点详情。
func (s *Service) Detail(ctx context.Context, req *permissiondto.DetailReq) (res *permissiondto.DetailResp, err error) {
	res = &permissiondto.DetailResp{}
	err = s.pm.DB(ctx).
		Select("id", "permission_code", "permission_name", "module", "api_path", "api_method",
			"status", "remark", "create_time", "update_time").
		Where("id = ?", req.ID).
		Scan(res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(permissionenums.ErrPermissionNotFound)
		}
		return nil, err
	}
	if res.ID == 0 {
		return nil, errors.New(permissionenums.ErrPermissionNotFound)
	}
	return res, nil
}