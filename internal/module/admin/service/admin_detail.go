package adminservice

import (
	"context"
	"errors"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"

	"gorm.io/gorm"
)

// 查询管理员详情
func (s *Service) Detail(ctx context.Context, req *admindto.DetailReq) (res *admindto.DetailResp, err error) {
	res = &admindto.DetailResp{}

	err = s.am.DB(ctx).
		Select("id", "username", "avatar", "email", "phone", "status", "is_admin",
			"register_ip", "register_location", "last_login_ip", "last_login_location",
			"last_login_time", "create_by", "create_time", "remark").
		Where("id = ?", req.Id).
		Scan(res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrAdminNotFound)
		}
		return nil, err
	}

	res.Roles = []any{}
	res.Menus = []any{}
	return
}
