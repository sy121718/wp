package userservice

// user_guest_account.go — 访客下单时自动开号。
//
// **一条不可越过的安全边界**：邮箱已存在时，这里什么都不改、只返回既有账号。
// 若在「邮箱已存在」时也重置密码并发邮件，那么任何人只要拿别人的邮箱下一单，
// 就能把对方密码换成自己知道的那一串 —— 那是完整的账户接管，而且从外部看
// 整条链路（下单 → 收到邮件）完全正常，不会有任何报错。

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	mailcontract "go_wp/internal/module/mail/contract"
	usercontract "go_wp/internal/module/user/contract"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
)

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

// EnsureGuestAccount 确保 email 对应账号存在；不存在才建号并发初始密码。
func (s *Service) EnsureGuestAccount(ctx context.Context, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
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
	existing, gerr := s.m.GetByEmail(ctx, email)
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
		Username: s.uniqueGuestUsername(ctx, email),
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
	if err = s.m.Create(ctx, e); err != nil {
		// 并发下单同一邮箱：唯一索引挡住后来者，此时重查返回既有账号而不是报错 ——
		// 对调用方（下单）而言「账号已存在」本来就是成功的一种形态。
		if again, aerr := s.m.GetByEmail(ctx, email); aerr == nil && again != nil && again.ID != 0 {
			return &usercontract.GuestAccountResult{
				UserID: again.ID, Username: again.Username, Created: false,
			}, nil
		}
		return nil, err
	}

	res = &usercontract.GuestAccountResult{
		UserID: e.ID, Username: e.Username, Created: true,
	}
	res.PasswordMailed = s.sendGuestAccountMail(ctx, email, displayName(e), e.Username, password, in.Locale)
	return res, nil
}

// sendGuestAccountMail 发初始密码邮件，返回是否已受理。
//
// 发信失败**不回滚建号**：账号已经存在、密码也已经设好，客户下次可以走「忘记密码」
// 自己重置；回滚会让「SMTP 抽风」变成「下单失败」，代价大得多。
func (s *Service) sendGuestAccountMail(ctx context.Context, email, name, username, password, locale string) bool {
	if s.mail == nil {
		return false
	}
	_, err := s.mail.SendTransactional(ctx, &mailcontract.SendInput{
		TemplateKey: guestAccountTemplate,
		Locale:      locale,
		To:          email,
		Vars: map[string]any{
			"name":      name,
			"username":  username,
			"password":  password,
			"site_name": s.siteName,
		},
	})
	return err == nil
}

// uniqueGuestUsername 由邮箱前缀生成一个未被占用的用户名。
//
// 冲突时追加随机后缀而不是报错：这是自动路径，报错会让「访客下单」失败在一件
// 客户完全无从处理的事情上（他的名字被别人注册过）。
func (s *Service) uniqueGuestUsername(ctx context.Context, email string) string {
	base := sanitizeUsername(strings.SplitN(email, "@", 2)[0])
	if base == "" {
		base = "user"
	}
	if len(base) > guestUsernameMaxLen {
		base = base[:guestUsernameMaxLen]
	}
	if _, err := s.m.GetByUsername(ctx, base); errors.Is(err, gorm.ErrRecordNotFound) {
		return base
	}
	for i := 0; i < 6; i++ {
		suffix, rerr := randomGuestToken(4)
		if rerr != nil {
			break
		}
		candidate := base + "_" + suffix
		if _, err := s.m.GetByUsername(ctx, candidate); errors.Is(err, gorm.ErrRecordNotFound) {
			return candidate
		}
	}
	// 兜底：时间后缀（重试六次全撞上在实践中不会发生）
	return fmt.Sprintf("%s_%d", base, time.Now().UnixNano()%1000000)
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
