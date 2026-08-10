package adminservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	"go_wp/pkg/captcha"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Login 管理员登录，返回 token 和用户信息。
//
// 流程：
//  1. 验证图形验证码
//  2. 查数据库，找不到用户返回模糊错误
//  3. 检查是否被锁定 / 被动禁用
//  4. bcrypt 对比密码
//  5. 失败：累加失败次数，连续 5 次后封禁 30 分钟
//  6. 成功：清空失败状态，记录登录 IP 和时间，生成 token 对
func (s *Service) Login(ctx context.Context, req *admindto.LoginReq, clientIP string) (*admindto.LoginResp, error) {
	// 1) 验证验证码
	var captchaSvc = captcha.Get()
	// Verify-验证验证码是否正确
	if !captchaSvc.Verify(req.CaptchaID, req.Captcha, true) {
		return nil, errors.New(adminenums.ErrCaptchaExpired)
	}

	// 2) 按用户名查用户（区分大小写）
	var entity adminmodel.AdminEntity
	//go特色，if先写短函数，然后再定义条件，当前是捕获err，如果err不为空，那就找错误，
	// 第一层错误是发牛的nil和系统报错，第二层是捕获gorm的报错然后替代默认的err
	if err := s.am.DB(ctx).Where("(BINARY username = ? OR email = ?)", req.Username, req.Username).First(&entity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrBadCredentials)
		}
		return nil, err
	}

	// 3) 检查是否被锁定
	if entity.IsLocked() {
		return nil, fmt.Errorf(adminenums.ErrAccountLocked,
			time.Until(*entity.LockedUntilTime).Round(time.Minute).String())
	}

	// 4) 检查是否被禁用
	if !entity.IsActive() {
		return nil, errors.New(adminenums.ErrAccountDisabled)
	}

	// 5) 密码校验
	if err := bcrypt.CompareHashAndPassword([]byte(entity.Password), []byte(req.Password)); err != nil {
		recordLoginFailure(ctx, s.am, &entity)
		return nil, errors.New(adminenums.ErrBadCredentials)
	}

	// 6) 登录成功，清空失败状态，记录登录信息
	now := time.Now()            //获取当前时间
	entity.LoginFailureCount = 0 //登录失败次数清空为0
	entity.LockedUntilTime = nil //清空封禁时间
	entity.LastFailureTime = nil
	entity.LastLoginTime = &now //设置登录时间
	if clientIP != "" {
		entity.LastLoginIP = &clientIP
	}
	if err := s.am.DB(ctx).Where("id = ?", entity.ID).Select(
		"login_failure_count", "locked_until_time", "last_failure_time",
		"last_login_time", "last_login_ip").
		Updates(&entity).Error; err != nil {
		return nil, err
	}

	// 7) 生成 token
	accessToken, sessionID, err := auth.GenerateSessionToken(int64(entity.ID), entity.Username, req.RememberMe, "")
	if err != nil {
		return nil, fmt.Errorf("生成 token 失败: %w", err)
	}

	// 8) 写入 Redis 会话
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

	if err := auth.SaveUserSession(ctx, &auth.UserSession{
		ID:        entity.ID,
		SessionID: sessionID,
		Username:  entity.Username,
		Name:      name,
		Avatar:    avatar,
		Email:     email,
		Phone:     phone,
		Status:    entity.Status,
		IsAdmin:   entity.IsAdmin,
		DeptID:    entity.DeptID,
	}, 0); err != nil {
		return nil, fmt.Errorf("写入用户会话失败: %w", err)
	}

	// 9) 刷新在线心跳
	if err := auth.RefreshOnline(ctx, entity.ID, 0); err != nil {
		return nil, fmt.Errorf("刷新在线状态失败: %w", err)
	}

	return &admindto.LoginResp{
		AccessToken: accessToken,
	}, nil
}

// recordLoginFailure 记录登录失败：累加次数，连续 5 次封禁 30 分钟。
func recordLoginFailure(ctx context.Context, am *adminmodel.AdminModel, entity *adminmodel.AdminEntity) {
	now := time.Now()
	entity.LoginFailureCount++
	entity.LastFailureTime = &now

	if entity.LoginFailureCount >= 5 {
		entity.Status = adminmodel.AdminStatusBanned
		lockedUntil := now.Add(30 * time.Minute)
		entity.LockedUntilTime = &lockedUntil
	}

	am.DB(ctx).Where("id = ?", entity.ID).
		Select("login_failure_count", "last_failure_time", "status", "locked_until_time").
		Updates(entity)
}
