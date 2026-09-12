package userservice

// user_password_reset.go — 密码重置（issue #36）。
//
// 与注册验证共用 activation_key 这一列，但**用途不同**，所以不能互相顶掉对方的凭据：
//   · 注册验证：账号是 pending，激活后置 active；
//   · 密码重置：账号是 active，改密后仍是 active。
// 两条流程互斥（pending 的账号谈不上「忘记密码」，active 的账号不需要继续验证邮箱），
// 所以共用一列是安全的；但代码里必须各自校验状态，不能只凭「码对得上」就放行。
//
// 三条安全判断：
//
//  1. **不泄露「邮箱是否已注册」**：无论邮箱存在与否，接口都返回成功。
//     否则这个接口就成了账号枚举器（试邮箱、看差异、拿到有效账号列表）。
//  2. **改密成功即清掉所有会话**的代理由会话层负责；这里至少清掉重置凭据，防止同一个码复用。
//  3. **重置码不返回给调用方**，理由同注册。

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	maildto "go_wp/internal/module/mail/dto"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/crypto"
)

// passwordResetTemplate 密码重置邮件模板 key（见迁移 129）。
const passwordResetTemplate = "password_reset"

// RequestPasswordReset 申请重置密码。
//
// **无论邮箱是否存在都返回成功**：这个接口如果对不存在与存在的邮箱给出不同回应，
// 就成了账号枚举器 —— 攻击者拿一批邮箱试一遍，就能筛出哪些是本站用户，
// 再去撞库或发钓鱼邮件。
func (s *Service) RequestPasswordReset(ctx context.Context, req *userdto.PasswordResetReqRequest) (err error) {
	if req == nil || strings.TrimSpace(req.Email) == "" {
		return errors.New(userenums.ErrInvalidParam)
	}
	email := strings.TrimSpace(req.Email)
	e, ferr := s.m.GetByEmail(ctx, email)
	if ferr != nil || e.Status != usermodel.UserStatusActive {
		// 不存在 / 未激活 / 已禁用：静默返回成功，不发信也不报错。
		return nil
	}

	plainKey, hashKey, kerr := newActivationKey()
	if kerr != nil {
		return kerr
	}
	expires := time.Now().Add(activationTTL)
	if uerr := s.m.UpdateFields(ctx, e.ID, map[string]any{
		"activation_key":        hashKey,
		"activation_expires_at": expires,
		"update_time":           time.Now(),
	}); uerr != nil {
		return uerr
	}
	if s.mail == nil {
		return nil
	}
	_, sendErr := s.mail.SendTemplate(ctx, &maildto.SendTemplateReq{
		TemplateKey: passwordResetTemplate,
		Locale:      req.Locale,
		To:          e.Email,
		Vars: map[string]any{
			"name":           displayName(e),
			"code":           plainKey,
			"expire_minutes": int(activationTTL / time.Minute),
		},
	})
	// 发信失败不向调用方暴露（同样是为了不泄露账号是否存在）。
	_ = sendErr
	return nil
}

// ResetPassword 用重置码改密码。
func (s *Service) ResetPassword(ctx context.Context, req *userdto.ResetPasswordReq) (err error) {
	if req == nil || strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Key) == "" {
		return errors.New(userenums.ErrInvalidParam)
	}
	if len(req.NewPassword) < minPasswordLen {
		return errors.New(userenums.ErrPasswordTooShort)
	}
	e, ferr := s.m.GetByEmail(ctx, strings.TrimSpace(req.Email))
	if ferr != nil {
		return errors.New(userenums.ErrActivationInvalid)
	}
	// 必须同时校验「码」与「账号状态」：码对但账号未激活（这是注册验证的码）不该能改密码。
	if e.ActivationKey == nil || *e.ActivationKey != crypto.Sha256(strings.ToUpper(strings.TrimSpace(req.Key))) {
		return errors.New(userenums.ErrActivationInvalid)
	}
	if e.ActivationExpiresAt != nil && time.Now().After(*e.ActivationExpiresAt) {
		return errors.New(userenums.ErrActivationInvalid)
	}
	if e.Status != usermodel.UserStatusActive {
		return errors.New(userenums.ErrActivationInvalid)
	}
	hashed, herr := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if herr != nil {
		return herr
	}
	now := time.Now()
	return s.m.UpdateFields(ctx, e.ID, map[string]any{
		"password":              string(hashed),
		"activation_key":        nil,
		"activation_expires_at": nil,
		// 改密顺带清掉登录失败锁定：用户已经证明了自己是账号所有者，
		// 让他继续被「失败次数过多」挡在门外没有意义。
		"login_failure_count": 0,
		"locked_until_time":   nil,
		"update_time":         now,
	})
}
