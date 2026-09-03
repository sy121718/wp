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
	"go_wp/pkg/logger"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AdminLogin 管理员登录，返回会话信息（由 handler 写入 cookie session）。
//
// 流程：
//  1. 验证图形验证码
//  2. 查数据库，找不到用户返回模糊错误
//  3. 检查是否被锁定 / 被动禁用
//  4. bcrypt 对比密码
//  5. 失败：累加失败次数，连续 5 次后封禁 30 分钟
//  6. 成功：清空失败状态，记录登录 IP 和时间，生成会话 ID 并写入 Redis
func (s *Service) AdminLogin(ctx context.Context, req *admindto.AdminLoginReq, clientIP string) (*admindto.AdminLoginResp, error) {
	// 1) 验证验证码
	var captchaSvc = captcha.Get()
	// Verify-验证验证码是否正确
	if !captchaSvc.Verify(req.CaptchaID, req.Captcha, true) {
		return nil, errors.New(adminenums.ErrCaptchaExpired)
	}

	// 2) 按用户名查用户（区分大小写）
	var entity adminmodel.AdminEntity
	// 二进制比较保证大小写敏感：MySQL 用 BINARY；
	// PostgreSQL 默认 collation 本身大小写敏感，直接等值比较。
	binaryExpr := "CAST(username AS BINARY) = CAST(? AS BINARY)"
	switch s.am.DialectName() {
	case "postgres":
		binaryExpr = "username = ?"
	}
	if err := s.am.DB(ctx).Where(binaryExpr+" OR email = ?", req.Username, req.Username).First(&entity).Error; err != nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "用户不存在").Warn("登录失败")
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(adminenums.ErrBadCredentials)
		}
		return nil, err
	}

	// 3) 检查是否被锁定
	if entity.IsLocked() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已封禁").Warn("登录失败")
		// key|param 协议：ErrAccountLocked 翻译模板含 %s，参数随错误消息传递（pkg/response 统一格式化）
		return nil, fmt.Errorf("%s|%s", adminenums.ErrAccountLocked,
			time.Until(*entity.LockedUntilTime).Round(time.Minute).String())
	}

	// 4) 检查是否被禁用
	if !entity.IsActive() {
		logger.Scene("admin").With("username", req.Username).With("reason", "账号已禁用").Warn("登录失败")
		return nil, errors.New(adminenums.ErrAccountDisabled)
	}

	// 5) 密码校验
	if err := bcrypt.CompareHashAndPassword([]byte(entity.Password), []byte(req.Password)); err != nil {
		logger.Scene("admin").With("username", req.Username).With("reason", "密码错误").Warn("登录失败")
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

	// 7) 生成会话 ID（cookie 会话与 Redis 用户会话绑定）
	sessionID, err := auth.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("生成会话 ID 失败: %w", err)
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

	logger.Scene("admin").With("username", entity.Username).Info("登录成功")
	return &admindto.AdminLoginResp{
		UserID:     entity.ID,
		Username:   entity.Username,
		SessionID:  sessionID,
		IssuedAt:   time.Now().Unix(),
		RememberMe: req.RememberMe,
	}, nil
}

// AdminLogout 注销当前管理员会话，使该会话立即失效。
func (s *Service) AdminLogout(ctx context.Context, userID uint64) (err error) {
	return auth.DeleteUserSession(ctx, userID)
}

// AdminProfile 获取当前登录用户信息。
// 优先从 Redis 读取，不存在时查询数据库并回填 Redis。
func (s *Service) AdminProfile(ctx context.Context, userID uint64) (*admindto.AdminProfileResp, error) {
	// 1) 优先从 Redis 获取会话
	session, err := auth.GetUserSession(ctx, userID)
	if err == nil && session != nil {
		return &admindto.AdminProfileResp{
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

	return &admindto.AdminProfileResp{
		ID:       entity.ID,
		Username: entity.Username,
		Name:     name,
		Avatar:   avatar,
		Email:    email,
		Phone:    phone,
		Status:   entity.Status,
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

	if err := am.DB(ctx).Where("id = ?", entity.ID).
		Select("login_failure_count", "last_failure_time", "status", "locked_until_time").
		Updates(entity).Error; err != nil {
		logger.Scene("admin").Error(err, "记录登录失败状态失败")
	}
}
