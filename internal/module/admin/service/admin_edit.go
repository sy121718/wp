package adminservice

import (
	"context"
	"errors"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	"go_wp/pkg/database"
)

func (s *Service) Edit(ctx context.Context, req *admindto.EditReq) (res *admindto.EditResp, err error) {
	entityExists, err := s.am.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if entityExists == nil {
		return nil, errors.New(adminenums.ErrAdminNotFound)
	}

	//判断邮箱唯一
	if emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email, req.Id); err != nil {
		return nil, err
	} else if emailExists {
		return nil, errors.New(adminenums.ErrEmailExists)
	}

	// 判断用户名唯一
	if nameExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "username", req.Username, req.Id); err != nil {
		return nil, err
	} else if nameExists {
		return nil, errors.New(adminenums.ErrUsernameExists)
	}

	// 构造实体
	// *string 类型的字段不能直接赋 string 值，需要用 & 取地址
	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Email:    &req.Email,
	}
	if req.Phone != "" {
		// 判断手机号码是否唯一
		if phoneExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "phone", req.Phone, req.Id); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, errors.New(adminenums.ErrPhoneExists)
		}
		entity.Phone = &req.Phone
	}

	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	// 执行更新
	if err := s.am.DB(ctx).Where("id = ?", req.Id).Updates(entity).Error; err != nil {
		return nil, err
	}
	if err := auth.DeleteUserSession(ctx, req.Id); err != nil {
		return nil, err
	}

	res = &admindto.EditResp{
		ID: req.Id,
	}
	return
}
