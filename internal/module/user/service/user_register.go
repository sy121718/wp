package userservice

// user_register.go — 注册与邮箱验证（issue #36）。
//
// 这是邮件模块的**第一个真实消费者**：模板写好了不等于被用上了，
// 注册验证走通才算「邮件能力真的在业务里跑起来」。
//
// 三个安全判断：
//
//  1. **激活码存 hash 不存明文**：它是凭据，与密码同级别。库被读走时明文凭据等于可直接激活账号。
//     校验时把用户输入的码 hash 后等值比对（等值查询仍然可用，因为没有加盐的随机码本身足够长）。
//  2. **码长 8 位字母数字而不是 6 位纯数字**：6 位数字只有 100 万种组合，在「无尝试次数限制」
//     的前提下可以被脚本跑穿（本项目尚未给验证码入口做限流）。
//  3. **接口不返回激活码**：那是用户邮箱里的东西；接口回传它，任何调用方都能直接激活，
//     验证邮箱这一步就白做了。

import (
	"context"
	"crypto/rand"
	"errors"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	maildto "go_wp/internal/module/mail/dto"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/crypto"
)

const (
	// activationKeyLen 激活码长度（字母数字混合）。
	activationKeyLen = 8
	// activationTTL 激活码有效期。
	activationTTL = 30 * time.Minute
	// minPasswordLen 密码最短长度。
	minPasswordLen = 8
	// registerVerifyTemplate 注册验证邮件模板 key（见迁移 129）。
	registerVerifyTemplate = "register_verify"
	// welcomeTemplate 激活成功后的欢迎邮件模板 key（见迁移 129）。
	welcomeTemplate = "welcome"
)

// Register 注册：写用户（待激活）并发验证邮件。
func (s *Service) Register(ctx context.Context, req *userdto.RegisterReq) (res *userdto.RegisterResp, err error) {
	if req == nil {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	if err = validateRegister(username, email, req.Password); err != nil {
		return nil, err
	}

	// 查重**包含已注销的账号**：注销是软删除、库里的唯一索引仍占着这个名字，
	// 只看未注销会报「可用」却在插入时撞唯一索引。
	exists, err := s.m.CountByExistence(ctx, username, email, 0)
	if err != nil {
		return nil, err
	}
	if exists > 0 {
		// 不区分「用户名占用」还是「邮箱占用」会在注册接口上暴露「这个邮箱注册过没有」；
		// 但注册接口本身就要给用户可操作的提示，所以这里如实说 —— 这是产品取舍，不是疏漏。
		if _, eerr := s.m.GetByUsername(ctx, username); eerr == nil {
			return nil, errors.New(userenums.ErrUsernameTaken)
		}
		return nil, errors.New(userenums.ErrEmailTaken)
	}

	plainKey, hashKey, err := newActivationKey()
	if err != nil {
		return nil, err
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expires := now.Add(activationTTL)
	e := &usermodel.UserEntity{
		Username:            username,
		Email:               email,
		Password:            string(hashed),
		Status:              usermodel.UserStatusPending,
		ActivationKey:       &hashKey,
		ActivationExpiresAt: &expires,
		RegisteredAt:        &now,
	}
	if nick := strings.TrimSpace(req.Nickname); nick != "" {
		e.Nickname = &nick
	}
	if ip := strings.TrimSpace(req.RegisterIP); ip != "" {
		e.RegisterIP = &ip
	}
	if loc := strings.TrimSpace(req.RegisterLocation); loc != "" {
		e.RegisterLocation = &loc
	}
	if err = s.m.Create(ctx, e); err != nil {
		return nil, err
	}

	res = &userdto.RegisterResp{
		UserID: e.ID, Username: username, Email: email, Status: e.Status,
	}
	// 发验证邮件：**失败不回滚注册**。回滚会让「SMTP 临时抽风」变成「用户注册不了」，
	// 而注册本身已经成功、验证邮件可以重发。如实把结果返回给调用方。
	res.MailQueued = s.sendVerifyMail(ctx, email, username, req.Nickname, plainKey, req.Locale)
	return res, nil
}

// ActivateEmail 校验激活码并激活账号。
func (s *Service) ActivateEmail(ctx context.Context, req *userdto.ActivateEmailReq) (res *userdto.ActivateResp, err error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	key := strings.ToUpper(strings.TrimSpace(req.Key))
	e, err := s.m.GetByActivationKey(ctx, crypto.Sha256(key))
	if err != nil {
		return nil, errors.New(userenums.ErrActivationInvalid)
	}
	if e.ActivationExpiresAt != nil && time.Now().After(*e.ActivationExpiresAt) {
		return nil, errors.New(userenums.ErrActivationInvalid)
	}
	now := time.Now()
	if uerr := s.m.UpdateFields(ctx, e.ID, map[string]any{
		"status":            usermodel.UserStatusActive,
		"email_verified_at": now,
		// 用掉即失效：清掉激活凭据，避免同一个码被重复使用。
		"activation_key":        nil,
		"activation_expires_at": nil,
		"update_time":           now,
	}); uerr != nil {
		return nil, uerr
	}
	// 激活成功后发欢迎邮件（同样不影响主流程）。
	s.sendWelcomeMail(ctx, e.Email, displayName(e), req.Locale)
	return &userdto.ActivateResp{UserID: e.ID, Username: e.Username}, nil
}

// ResendActivation 重发验证邮件（仅对未激活的账号，且重新签发激活码）。
//
// 与 RequestPasswordReset 的差别（**有意，不是疏漏**）：这里对「邮箱不存在」明确报错，
// 而密码重置一律静默成功。判断依据是这个接口**不新增任何信息**：
// 注册接口本来就会回「邮箱已被注册」，注册与否本来就能被逐个试出来，
// 这里再掩饰一次不会提高攻击成本，只会让真实用户在「我到底注册没注册过」上多绕一圈。
// 密码重置不同 —— 它只需要一个邮箱、匿名可调，静默与否直接决定它能不能当批量枚举器用。
func (s *Service) ResendActivation(ctx context.Context, req *userdto.ResendActivationReq) (err error) {
	if req == nil || strings.TrimSpace(req.Email) == "" {
		return errors.New(userenums.ErrInvalidParam)
	}
	e, err := s.m.GetByEmail(ctx, strings.TrimSpace(req.Email))
	if err != nil {
		return errors.New(userenums.ErrUserNotFound)
	}
	if e.Status != usermodel.UserStatusPending {
		// 已激活的账号没有「重发验证」这回事 —— 静默成功会让调用方以为发了信。
		return errors.New(userenums.ErrActivationInvalid)
	}
	plainKey, hashKey, err := newActivationKey()
	if err != nil {
		return err
	}
	expires := time.Now().Add(activationTTL)
	if uerr := s.m.UpdateFields(ctx, e.ID, map[string]any{
		"activation_key":        hashKey,
		"activation_expires_at": expires,
		"update_time":           time.Now(),
	}); uerr != nil {
		return uerr
	}
	if !s.sendVerifyMail(ctx, e.Email, e.Username, displayName(e), plainKey, req.Locale) {
		return errors.New(userenums.ErrMailUnavailable)
	}
	return nil
}

// sendVerifyMail 发注册验证邮件，返回是否已受理。
func (s *Service) sendVerifyMail(ctx context.Context, email, username, nickname, code, locale string) bool {
	if s.mail == nil {
		return false
	}
	_, err := s.mail.SendTemplate(ctx, &maildto.SendTemplateReq{
		TemplateKey: registerVerifyTemplate,
		Locale:      locale,
		To:          email,
		Vars: map[string]any{
			"name":           firstNonEmpty(nickname, username),
			"code":           code,
			"expire_minutes": int(activationTTL / time.Minute),
		},
	})
	return err == nil
}

// sendWelcomeMail 发欢迎邮件（激活之后，失败不影响激活结果）。
func (s *Service) sendWelcomeMail(ctx context.Context, email, name, locale string) {
	if s.mail == nil {
		return
	}
	_, _ = s.mail.SendTemplate(ctx, &maildto.SendTemplateReq{
		TemplateKey: welcomeTemplate,
		Locale:      locale,
		To:          email,
		Vars:        map[string]any{"name": name, "site_name": s.siteName},
	})
}

// newActivationKey 生成激活码：返回（给用户看的明文，存库的 hash）。
func newActivationKey() (plain, hash string, err error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去掉易混的 I/O/0/1
	buf := make([]byte, activationKeyLen)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	b := make([]byte, activationKeyLen)
	for i := range buf {
		b[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	plain = string(b)
	return plain, crypto.Sha256(plain), nil
}

func validateRegister(username, email, password string) error {
	if username == "" {
		return errors.New(userenums.ErrUsernameRequired)
	}
	if len([]rune(username)) < 3 {
		return errors.New(userenums.ErrUsernameTooShort)
	}
	if len([]rune(username)) > 32 {
		return errors.New(userenums.ErrUsernameTooLong)
	}
	if email == "" {
		return errors.New(userenums.ErrEmailRequired)
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return errors.New(userenums.ErrEmailInvalid)
	}
	if len(password) < minPasswordLen {
		return errors.New(userenums.ErrPasswordTooShort)
	}
	return nil
}

func displayName(e *usermodel.UserEntity) string {
	if e.Nickname != nil && strings.TrimSpace(*e.Nickname) != "" {
		return strings.TrimSpace(*e.Nickname)
	}
	return e.Username
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
