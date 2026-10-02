package userservice

// user_guest_account.go — 访客下单时自动开号。
//
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
