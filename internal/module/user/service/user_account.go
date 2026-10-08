package userservice

// 三件事：看自己的账号、改资料与偏好、改密码。
// 共同的一条原则：**userID 一律由调用方（inbound）从会话里取，绝不从请求参数取**。
// 从请求里取 id 的账号接口等于「谁都能改别人的资料」，而且它看起来完全正常。

// 这里**不新增任何查询逻辑**，只是把 service 已有的两个读方法暴露成收窄端口：
// GetAccount / ListSessions 是账号中心页面在用的同一份实现。
// 片段层因此看到的数据与内置页面逐字节一致 —— 两套实现各自演化，
// 表现就是「页面上的昵称和片段里的昵称不一样」，而这种差异没人会主动去查。

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

// **一条不可越过的安全边界**：邮箱已存在时，这里什么都不改、只返回既有账号。
// 若在「邮箱已存在」时也重置密码并发邮件，那么任何人只要拿别人的邮箱下一单，
// 就能把对方密码换成自己知道的那一串 —— 那是完整的账户接管，而且从外部看
// 整条链路（下单 → 收到邮件）完全正常，不会有任何报错。
//
// BIZ-11：开号必须能与**建单的事务**合流。建号成功、订单因库存不足回滚，会留下一个
// 能登录却没有订单的孤儿账号（客户还收到了初始密码邮件）。因此本文件有两条入口：
//
//	· EnsureGuestAccount    —— 自足事务（建号 + 发信），既有前台路径不变；
//	· EnsureGuestAccountTx  —— 落在调用方事务里，**只建号、不发信**：
//	                           邮件载荷经结果带出，由调用方在提交后调 SendGuestAccountMail。

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
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go_wp/internal/module/mail/contract"
	"go_wp/internal/module/user/contract"
	"go_wp/internal/module/user/dto"
	"go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
)

// 生日只用到日期：解析格式固定 `2006-01-02`，解析不了一律拒绝而不是「静默忽略」——
// 静默忽略会让用户以为生日存上了，下次打开却是空的。
const birthdayLayout = "2006-01-02"

// 偏好里 pageSize 的允许范围。
const (
	minPageSize = 1
	maxPageSize = 200
)

// displayTimeLayout 页面上显示时间的统一格式。
const displayTimeLayout = "2006-01-02 15:04"

// formatTime 把可空时间格式化成页面文本。
//
// 空值显示成破折号而不是空串：留白会让人以为「这一栏坏了」，
// 而「没有登录记录」与「这一栏坏了」对用户是两件事。
func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Format(displayTimeLayout)
}

// allowedProfileVisibility 主页可见性的合法取值（与迁移 123 的列注释一致）。
var allowedProfileVisibility = map[string]struct{}{
	"public":  {},
	"members": {},
	"private": {},
}

// GetAccount 取账号中心的一次性聚合视图。
func (s *Service) GetAccount(ctx context.Context, userID uint64) (res *userdto.AccountResp, err error) {
	if userID == 0 {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	u, gerr := s.m.GetByID(ctx, userID)
	if gerr != nil {
		return nil, errors.New(userenums.ErrUserNotFound)
	}

	res = &userdto.AccountResp{
		UserID:            u.ID,
		Username:          u.Username,
		Email:             u.Email,
		EmailVerified:     u.EmailVerifiedAt != nil,
		DisplayName:       displayName(u),
		Avatar:            deref(u.Avatar),
		Status:            u.Status,
		RegisteredAt:      utils.NewJSONTimePtr(u.RegisteredAt),
		LastLoginTime:     utils.NewJSONTimePtr(u.LastLoginTime),
		RegisteredAtText:  formatTime(u.RegisteredAt),
		LastLoginTimeText: formatTime(u.LastLoginTime),
		LastLoginIP:       deref(u.LastLoginIP),
		LastLoginLocation: deref(u.LastLoginLocation),
	}
	if u.Nickname != nil {
		res.Nickname = *u.Nickname
	}

	// 资料与偏好可能都还没建行（新注册用户就是这样）—— 这不是错误，返回零值即可。
	if s.pm != nil {
		p, perr := s.pm.GetByUserID(ctx, userID)
		if perr != nil {
			return nil, perr
		}
		if p != nil {
			res.FirstName = deref(p.FirstName)
			res.LastName = deref(p.LastName)
			res.Gender = p.Gender
			if p.Birthday != nil {
				res.Birthday = p.Birthday.Format(birthdayLayout)
			}
			res.Bio = deref(p.Bio)
			res.Website = deref(p.Website)
			res.Country = deref(p.Country)
			res.Province = deref(p.Province)
			res.City = deref(p.City)
			res.Address = deref(p.Address)
			res.Postcode = deref(p.Postcode)
			res.Phone = deref(p.Phone)
			res.Company = deref(p.Company)
		}
	}
	if s.prefm != nil {
		pref, xerr := s.prefm.GetByUserID(ctx, userID)
		if xerr != nil {
			return nil, xerr
		}
		if pref != nil {
			res.Theme = deref(pref.Theme)
			res.Locale = deref(pref.Locale)
			res.Timezone = deref(pref.Timezone)
			res.PageSize = pref.PageSize
			res.EmailNotify = pref.EmailNotify
			res.SmsNotify = pref.SmsNotify
			res.ProfileVisibility = pref.ProfileVisibility
			res.ShowOnline = pref.ShowOnline
		} else {
			// 没有偏好行时给出与数据库默认值一致的展示值，
			// 而不是空串/0 —— 否则用户在界面上看到「每页 0 条」。
			res.PageSize = 20
			res.EmailNotify = true
			res.ShowOnline = true
			res.ProfileVisibility = "public"
		}
	}
	return res, nil
}

// UpdateProfile 保存资料：昵称写 users、其余写 user_profiles，**同一次提交**。
//
// 一次事务的理由：账号中心是「一个表单一次保存」，拆成两次写会出现
// 「昵称改了、资料没改」的半截状态，而用户看到的是一句「保存成功」。
func (s *Service) UpdateProfile(ctx context.Context, req *userdto.UpdateProfileReq) (err error) {
	if req == nil || req.UserID == 0 {
		return errors.New(userenums.ErrInvalidParam)
	}
	nickname := strings.TrimSpace(req.Nickname)
	if nickname != "" {
		if n := len([]rune(nickname)); n > 60 {
			return errors.New(userenums.ErrUsernameTooLong)
		}
	}

	var birthday *time.Time
	if b := strings.TrimSpace(req.Birthday); b != "" {
		t, perr := time.Parse(birthdayLayout, b)
		if perr != nil {
			return errors.New(userenums.ErrInvalidParam)
		}
		birthday = &t
	}

	now := time.Now()
	// 空串统一按「清空该字段」处理：账号中心的表单会把未填的字段原样提交为空串，
	// 而用户清空一个字段的意图就是清空它。「没提交这个字段」在这里无法与「提交了空值」区分，
	// 因此表单必须把它管理的所有字段都带上（模板里用隐藏域保证这一点）。
	profile := &usermodel.UserProfileEntity{
		UserID:     req.UserID,
		FirstName:  optString(req.FirstName),
		LastName:   optString(req.LastName),
		Gender:     req.Gender,
		Birthday:   birthday,
		Bio:        optString(req.Bio),
		Website:    optString(req.Website),
		Locale:     optString(req.Locale),
		Timezone:   optString(req.Timezone),
		Country:    optString(req.Country),
		Province:   optString(req.Province),
		City:       optString(req.City),
		Address:    optString(req.Address),
		Postcode:   optString(req.Postcode),
		Phone:      optString(req.Phone),
		Company:    optString(req.Company),
		UpdateTime: &now,
	}
	fields := map[string]any{
		"nickname":    optString(nickname),
		"update_time": now,
	}

	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.m.UpdateFieldsTx(ctx, tx, req.UserID, fields); uerr != nil {
			return uerr
		}
		if s.pm == nil {
			return nil
		}
		return s.pm.UpsertTx(ctx, tx, profile)
	})
}

// UpdatePreference 保存前台偏好。
func (s *Service) UpdatePreference(ctx context.Context, req *userdto.UpdatePreferenceReq) (err error) {
	if req == nil || req.UserID == 0 {
		return errors.New(userenums.ErrInvalidParam)
	}
	if req.PageSize < minPageSize || req.PageSize > maxPageSize {
		return errors.New(userenums.ErrPageSizeInvalid)
	}
	vis := strings.TrimSpace(req.ProfileVisibility)
	if vis == "" {
		vis = "public"
	}
	if _, ok := allowedProfileVisibility[vis]; !ok {
		return errors.New(userenums.ErrProfileVisibilityInvalid)
	}
	if s.prefm == nil {
		return errors.New(userenums.ErrInvalidParam)
	}
	now := time.Now()
	return s.prefm.Upsert(ctx, &usermodel.UserPreferenceEntity{
		UserID:            req.UserID,
		Theme:             optString(req.Theme),
		Locale:            optString(req.Locale),
		Timezone:          optString(req.Timezone),
		PageSize:          req.PageSize,
		EmailNotify:       req.EmailNotify,
		SmsNotify:         req.SmsNotify,
		ProfileVisibility: vis,
		ShowOnline:        req.ShowOnline,
		UpdateTime:        &now,
	})
}

// ChangePassword 已登录用户改密码（需验证当前密码）。
//
// 改完之后**撤销该用户的全部会话**，包括当前这一个：密码变更意味着
// 「之前的凭据全部作废」，把当前设备留着会让「我怀疑账号被盗、赶紧改密码」
// 这个动作达不到目的 —— 攻击者那一台如果是当前会话，改完密码它还在线。
// 代价是用户改完要重新登录一次，这个代价必须付。
func (s *Service) ChangePassword(ctx context.Context, req *userdto.ChangePasswordReq) (err error) {
	if req == nil || req.UserID == 0 {
		return errors.New(userenums.ErrInvalidParam)
	}
	if len(req.NewPassword) < minPasswordLen {
		return errors.New(userenums.ErrPasswordTooShort)
	}
	u, gerr := s.m.GetByID(ctx, req.UserID)
	if gerr != nil {
		return errors.New(userenums.ErrUserNotFound)
	}
	if strings.TrimSpace(u.Password) == "" {
		// 第三方账号没有本站密码：允许它「设置」密码要另有一条带验证的流程，
		// 不能走这里（这里要求提供旧密码，而它根本没有）。
		return errors.New(userenums.ErrPasswordLoginUnavailable)
	}
	if cerr := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.OldPassword)); cerr != nil {
		return errors.New(userenums.ErrOldPasswordWrong)
	}
	if req.OldPassword == req.NewPassword {
		return errors.New(userenums.ErrNewPasswordSame)
	}
	hashed, herr := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if herr != nil {
		return herr
	}
	if uerr := s.m.UpdateFields(ctx, req.UserID, map[string]any{
		"password":    string(hashed),
		"update_time": time.Now(),
	}); uerr != nil {
		return uerr
	}
	return s.RevokeAllSessions(ctx, req.UserID)
}

// 编译期断言：装配期注入的就是这个实现。
var _ usercontract.VisitorAccountPort = (*Service)(nil)

// AccountOf 读账号概览与资料 / 偏好。
func (s *Service) AccountOf(ctx context.Context, userID uint64) (res *userdto.AccountResp, err error) {
	return s.GetAccount(ctx, userID)
}

// SessionsOf 读登录设备台账（currentToken 用于标记当前设备）。
func (s *Service) SessionsOf(ctx context.Context, userID uint64, currentToken string) (items []*userdto.SessionItem, err error) {
	return s.ListSessions(ctx, userID, currentToken)
}

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
	_, sendErr := s.mail.SendTransactional(ctx, &mailcontract.SendInput{
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

const (
	// guestAccountPasswordLen 初始密码长度。
	//
	// 12 位、crypto/rand 生成：初始密码要经邮件发出去，可预测的初始密码等于没有密码
	// （尤其在他还没来得及改之前，这封邮件可能已经在邮箱里躺了很久）。
	guestAccountPasswordLen = 12
	// guestUsernameMaxLen 由邮箱前缀生成的用户名上限（username 列 varchar(60)，留足余量）。
	guestUsernameMaxLen = 24
	// guestAccountTemplate 初始密码邮件模板 key（见迁移 137）。
	guestAccountTemplate = "guest_account"
)

// 编译期断言：本 service 满足订单域需要的开号端口。
var _ usercontract.GuestAccountProvisioner = (*Service)(nil)

// LookupGuestAccount 按邮箱**只读**查账号（不开号、不发信）。
//
// 用途（BIZ-11 引入）：调用方在建单前解析「这个邮箱是不是已有账号」，好让会员折扣
// 按既有身份计算 —— 开号本身已经移进事务，事务前拿不到它的结果。
// 查不到返回 (0, false, nil)。
func (s *Service) LookupGuestAccount(ctx context.Context, email string) (userID uint64, found bool, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return 0, false, nil
	}
	e, gerr := s.m.GetByEmail(ctx, addr)
	if gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return 0, false, gerr
	}
	if gerr == nil && e != nil && e.ID != 0 {
		return e.ID, true, nil
	}
	return 0, false, nil
}

// EnsureGuestAccount 确保 email 对应账号存在；不存在才建号并发初始密码（自足事务）。
func (s *Service) EnsureGuestAccount(ctx context.Context, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
	res, err = s.ensureGuestAccountOn(ctx, nil, in)
	if err != nil {
		return nil, err
	}
	// 自足路径在这里把信发出去（事务已提交），随后清掉明文载荷 ——
	// 它没有理由继续留在调用方手里。
	if res.PendingMail != nil {
		res.PasswordMailed = s.sendGuestAccountMailOf(ctx, res.PendingMail)
		res.PendingMail = nil
	}
	return res, nil
}

// EnsureGuestAccountTx 与 EnsureGuestAccount 同语义，但写入落在**调用方的事务**里。
//
// **不发信**（事务未提交，发了回滚收不回）：邮件载荷放在结果的 PendingMail 里，
// 由调用方在事务提交后调 SendGuestAccountMail。见契约里的同方法注释。
func (s *Service) EnsureGuestAccountTx(ctx context.Context, tx *gorm.DB, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
	return s.ensureGuestAccountOn(ctx, tx, in)
}

// SendGuestAccountMail 事务提交后发出初始密码邮件（BIZ-11）。
//
// 返回 error 只表示「这封信没被受理」，供调用方记日志与回填响应里的 AccountMailed；
// **调用方不得因它回滚已提交的订单** —— 账号已经建好了，客户可以在登录页走
// 「忘记密码」自己重置。把发信失败升级成下单失败，代价远大于一封没寄出的邮件。
func (s *Service) SendGuestAccountMail(ctx context.Context, mail *usercontract.GuestAccountMail) error {
	if mail == nil {
		return nil
	}
	if s.sendGuestAccountMailOf(ctx, mail) {
		return nil
	}
	return errors.New("初始密码邮件未受理")
}

// ensureGuestAccountOn 开号的唯一实现：tx 为 nil 时走 model 的默认句柄（自足），
// 非 nil 时全部读写落在调用方事务内。**不发信** —— 发信时机由两个入口各自决定。
func (s *Service) ensureGuestAccountOn(ctx context.Context, tx *gorm.DB, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
	if in == nil {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	email := strings.TrimSpace(in.Email)
	if email == "" {
		return nil, errors.New(userenums.ErrEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return nil, errors.New(userenums.ErrEmailInvalid)
	}

	// 已有账号 → 什么都不改。
	// 注意 GetByEmail 在「查不到」时返回 gorm.ErrRecordNotFound 且实体非 nil，
	// 所以判断必须看 err 而不是 e。
	existing, gerr := s.findUserByEmail(ctx, tx, email)
	if gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	if gerr == nil && existing != nil && existing.ID != 0 {
		return &usercontract.GuestAccountResult{
			UserID: existing.ID, Username: existing.Username, Created: false,
		}, nil
	}

	password, perr := newGuestPassword()
	if perr != nil {
		return nil, perr
	}
	hashed, herr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if herr != nil {
		return nil, herr
	}
	now := time.Now()
	e := &usermodel.UserEntity{
		Username: s.uniqueGuestUsernameOn(ctx, tx, email),
		Email:    email,
		Password: string(hashed),
		// 直接可用：发初始密码的目的就是让他能登进来查订单，
		// 若置为「待激活」则还要先走邮箱验证，等于这封邮件白发了。
		Status:       usermodel.UserStatusActive,
		RegisteredAt: &now,
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		e.Nickname = &name
	}
	if ip := strings.TrimSpace(in.RegisterIP); ip != "" {
		e.RegisterIP = &ip
	}
	if err = s.createUser(ctx, tx, e); err != nil {
		// 并发下单同一邮箱：唯一索引挡住后来者，此时重查返回既有账号而不是报错 ——
		// 对调用方（下单）而言「账号已存在」本来就是成功的一种形态。
		if again, aerr := s.findUserByEmail(ctx, tx, email); aerr == nil && again != nil && again.ID != 0 {
			return &usercontract.GuestAccountResult{
				UserID: again.ID, Username: again.Username, Created: false,
			}, nil
		}
		return nil, err
	}

	return &usercontract.GuestAccountResult{
		UserID: e.ID, Username: e.Username, Created: true,
		// 发信载荷带出去，由调用方在**事务提交后**发出（见契约注释）。
		PendingMail: &usercontract.GuestAccountMail{
			Email: email, Name: displayName(e), Username: e.Username,
			Password: password, Locale: in.Locale,
		},
	}, nil
}

// findUserByEmail 按邮箱取账号（tx 非空时读在调用方事务内）。
func (s *Service) findUserByEmail(ctx context.Context, tx *gorm.DB, email string) (*usermodel.UserEntity, error) {
	if tx != nil {
		return s.m.GetByEmailTx(ctx, tx, email)
	}
	return s.m.GetByEmail(ctx, email)
}

// createUser 建号（tx 非空时写进调用方事务）。
func (s *Service) createUser(ctx context.Context, tx *gorm.DB, e *usermodel.UserEntity) error {
	if tx != nil {
		return s.m.CreateTx(ctx, tx, e)
	}
	return s.m.Create(ctx, e)
}

// sendGuestAccountMailOf 发信实现（载荷形式，见 sendGuestAccountMail）。
func (s *Service) sendGuestAccountMailOf(ctx context.Context, mail *usercontract.GuestAccountMail) bool {
	if s.mail == nil || mail == nil {
		return false
	}
	to := strings.TrimSpace(mail.Email)
	if to == "" || strings.TrimSpace(mail.Password) == "" {
		return false
	}
	name := strings.TrimSpace(mail.Name)
	if name == "" {
		name = mail.Username
	}
	_, err := s.mail.SendTransactional(ctx, &mailcontract.SendInput{
		TemplateKey: guestAccountTemplate,
		Locale:      mail.Locale,
		To:          to,
		Vars: map[string]any{
			"name":      name,
			"username":  mail.Username,
			"password":  mail.Password,
			"site_name": s.siteName,
		},
	})
	return err == nil
}

// uniqueGuestUsernameOn 同 uniqueGuestUsername，tx 非空时读在调用方事务内
// （事务内要能看到自己刚建的账号，不能另开连接去判重）。
func (s *Service) uniqueGuestUsernameOn(ctx context.Context, tx *gorm.DB, email string) string {
	base := sanitizeUsername(strings.SplitN(email, "@", 2)[0])
	if base == "" {
		base = "user"
	}
	if len(base) > guestUsernameMaxLen {
		base = base[:guestUsernameMaxLen]
	}
	if !s.usernameTaken(ctx, tx, base) {
		return base
	}
	for i := 0; i < 6; i++ {
		suffix, rerr := randomGuestToken(4)
		if rerr != nil {
			break
		}
		candidate := base + "_" + suffix
		if !s.usernameTaken(ctx, tx, candidate) {
			return candidate
		}
	}
	// 兜底：时间后缀（重试六次全撞上在实践中不会发生）
	return fmt.Sprintf("%s_%d", base, time.Now().UnixNano()%1000000)
}

// usernameTaken 用户名是否已被占用（tx 非空时读在调用方事务内）。
func (s *Service) usernameTaken(ctx context.Context, tx *gorm.DB, username string) bool {
	var err error
	if tx != nil {
		_, err = s.m.GetByUsernameTx(ctx, tx, username)
	} else {
		_, err = s.m.GetByUsername(ctx, username)
	}
	return err == nil
}

// sanitizeUsername 把邮箱前缀收敛成合法用户名（只留字母数字与下划线）。
func sanitizeUsername(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// newGuestPassword 生成随机初始密码。
//
// 字母表去掉 I/O/l/o/0/1：客户可能要手抄这串密码，易混字符会变成「明明照抄却登不上」。
func newGuestPassword() (string, error) {
	return randomGuestToken(guestAccountPasswordLen)
}

// randomGuestToken 从去混淆字母表里取 n 个字符。
func randomGuestToken(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

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
	_, err := s.mail.SendTransactional(ctx, &mailcontract.SendInput{
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
	_, _ = s.mail.SendTransactional(ctx, &mailcontract.SendInput{
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
