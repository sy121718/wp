package adminservice

import (
	"context"
	"errors"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/database"

	"golang.org/x/crypto/bcrypt"
)

func (s *Service) Create(ctx context.Context, req *admindto.CreateReq) (res *admindto.CreateResp, err error) {
	// // 检查邮箱是否已存在
	// var existCount int64
	// if err := s.am.DB(ctx).Where("email = ? AND deleted_time IS NULL", req.Email).Count(&existCount).Error; err != nil {
	// 	return nil, err
	// }
	// if existCount > 0 {
	// 	return nil, errors.New("该邮箱已被占用")
	// }
	if emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email); err != nil {
		return nil, err
	} else if emailExists {
		return nil, errors.New(adminenums.ErrEmailExists)
	}
	if nameExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "username", req.Username); err != nil {
		return nil, err
	} else if nameExists {
		return nil, errors.New(adminenums.ErrUsernameExists)
	}
	// emailExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "email", req.Email)
	// if err != nil {
	// 	fmt.Print("666")
	// 	return nil, err
	// }
	// if emailExists {
	// 	return nil, errors.New("该邮箱已存在")
	// }

	// 加密密码
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// 构造实体
	// *string 类型的字段不能直接赋 string 值，需要用 & 取地址
	entity := &adminmodel.AdminEntity{
		Username: req.Username,
		Password: string(hashed),
		Email:    &req.Email,
		Status:   adminmodel.AdminStatusActive,
		Name:     &req.Username, // Username 必填，直接赋值
	}
	// 只要有接收值并且可选字段的（omitempty）：有值才写，没值保持 nil → 数据库写 NULL，
	if req.Phone != "" {
		if phoneExists, err := database.IsFieldExists(s.am.DB(ctx), &adminmodel.AdminEntity{}, "phone", req.Phone); err != nil {
			return nil, err
		} else if phoneExists {
			return nil, errors.New(adminenums.ErrPhoneExists)
		}
		entity.Phone = &req.Phone
	}
	if req.Avatar != "" {
		entity.Avatar = &req.Avatar
	}

	if err := s.am.DB(ctx).Create(entity).Error; err != nil {
		return nil, err
	}
	res = &admindto.CreateResp{
		ID:       entity.ID,
		Username: entity.Username,
	}

	return
}
