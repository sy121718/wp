package adminservice

import (
	"context"
	"fmt"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/auth"
)

// Profile 获取当前登录用户信息。
// 优先从 Redis 读取，不存在时查询数据库并回填 Redis。
func (s *Service) Profile(ctx context.Context, userID uint64) (*admindto.ProfileResp, error) {
	// 1) 优先从 Redis 获取会话
	session, err := auth.GetUserSession(ctx, userID)
	if err == nil && session != nil {
		return &admindto.ProfileResp{
			ID:       session.ID,
			Username: session.Username,
			Name:     session.Name,
			Avatar:   session.Avatar,
			Email:    session.Email,
			Phone:    session.Phone,
			Status:   session.Status,
		}, nil
	}

	// 2) Redis 未命中，查询数据库
	entity, err := s.am.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, fmt.Errorf(adminenums.ErrUserNotFound)
	}

	name := ""
	if entity.Name != nil {
		name = *entity.Name
	}
	avatar := ""
	if entity.Avatar != nil {
		avatar = *entity.Avatar
	}
	email := ""
	if entity.Email != nil {
		email = *entity.Email
	}
	phone := ""
	if entity.Phone != nil {
		phone = *entity.Phone
	}

	// 3) 回填 Redis
	if err := auth.SaveUserSession(ctx, &auth.UserSession{
		ID:       entity.ID,
		Username: entity.Username,
		Name:     name,
		Avatar:   avatar,
		Email:    email,
		Phone:    phone,
		Status:   entity.Status,
		IsAdmin:  entity.IsAdmin,
		DeptID:   entity.DeptID,
	}, 0); err != nil {
		return nil, fmt.Errorf("写入用户会话失败: %w", err)
	}

	return &admindto.ProfileResp{
		ID:       entity.ID,
		Username: entity.Username,
		Name:     name,
		Avatar:   avatar,
		Email:    email,
		Phone:    phone,
		Status:   entity.Status,
	}, nil
}
