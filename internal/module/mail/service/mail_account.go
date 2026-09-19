package mailservice

// mail_account.go — 发信账号的 CRUD 与测试发送（issue #37）。
//
// 两处安全要点：
//   1. SMTP 密码**只以密文形式落库**（pkg/crypto 的 AES-GCM），解密只发生在发信那一刻；
//   2. 任何日志与响应都不得带出明文密码（本文件里密码变量只在构造 Sender 的局部作用域）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/crypto"
	"go_wp/pkg/mailer"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// SetCipherSecret 注入敏感配置加密密钥（装配期从 config 读入后调用）。
//
// 为空表示未配置：此时账号保存会明确报错，而不是用弱密钥悄悄加密 ——
// 用固定弱密钥加密等于把 SMTP 密码明文写在库里。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

// CreateAccount 新建发信账号（密码加密存储）。
func (s *Service) CreateAccount(ctx context.Context, req *maildto.SaveAccountReq) (res *maildto.AccountItem, err error) {
	if req == nil {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	if err = validateAccountReq(req, true); err != nil {
		return nil, err
	}
	cipher, err := s.encryptPassword(req.Password)
	if err != nil {
		return nil, err
	}
	purpose := normalizePurpose(req.Purpose)
	e := &mailmodel.MailAccountEntity{
		Name:           strings.TrimSpace(req.Name),
		Purpose:        purpose,
		FromEmail:      strings.TrimSpace(req.FromEmail),
		Provider:       normalizeProvider(req.Provider),
		Encryption:     normalizeEncryption(req.Encryption),
		Status:         mailmodel.AccountStatusEnabled,
		RatePerHour:    req.RatePerHour,
		PasswordCipher: &cipher,
	}
	if v := strings.TrimSpace(req.FromName); v != "" {
		e.FromName = &v
	}
	if v := strings.TrimSpace(req.ReplyTo); v != "" {
		e.ReplyTo = &v
	}
	if v := strings.TrimSpace(req.Host); v != "" {
		e.Host = &v
	}
	if req.Port > 0 {
		p := req.Port
		e.Port = &p
	}
	if v := strings.TrimSpace(req.Username); v != "" {
		e.Username = &v
	}
	// 第一个账号自动成为该用途的默认账号（否则事务邮件没有默认通道可用）。
	if _, derr := s.m.DefaultAccount(ctx, purpose); errors.Is(derr, gorm.ErrRecordNotFound) {
		e.IsDefault = true
	}
	if err = s.m.CreateAccount(ctx, e); err != nil {
		return nil, err
	}
	return accountItemOf(e), nil
}

// UpdateAccount 更新账号；**密码留空表示不改**（避免「编辑页面回显不了密码」时的误清空）。
func (s *Service) UpdateAccount(ctx context.Context, req *maildto.SaveAccountReq) (res *maildto.AccountItem, err error) {
	if req == nil || req.ID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	if err = validateAccountReq(req, false); err != nil {
		return nil, err
	}
	if _, err = s.m.GetAccount(ctx, req.ID); err != nil {
		return nil, errors.New(mailenums.ErrAccountNotFound)
	}
	fields := map[string]any{
		"name":          strings.TrimSpace(req.Name),
		"from_email":    strings.TrimSpace(req.FromEmail),
		"provider":      normalizeProvider(req.Provider),
		"encryption":    normalizeEncryption(req.Encryption),
		"rate_per_hour": req.RatePerHour,
		"from_name":     nullable(req.FromName),
		"reply_to":      nullable(req.ReplyTo),
		"host":          nullable(req.Host),
		"username":      nullable(req.Username),
		"update_time":   time.Now(),
	}
	if req.Port > 0 {
		fields["port"] = req.Port
	}
	if strings.TrimSpace(req.Password) != "" {
		cipher, cerr := s.encryptPassword(req.Password)
		if cerr != nil {
			return nil, cerr
		}
		fields["password_cipher"] = cipher
	}
	if err = s.m.UpdateAccountFields(ctx, req.ID, fields); err != nil {
		return nil, err
	}
	e, err := s.m.GetAccount(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return accountItemOf(e), nil
}

// ListAccounts 列出账号（响应里**不含任何密码字段**）。
func (s *Service) ListAccounts(ctx context.Context, purpose string) (res []*maildto.AccountItem, err error) {
	list, err := s.m.ListAccounts(ctx, purpose, false)
	if err != nil {
		return nil, err
	}
	res = make([]*maildto.AccountItem, 0, len(list))
	for _, e := range list {
		res = append(res, accountItemOf(e))
	}
	return res, nil
}

// DeleteAccount 删除账号。
func (s *Service) DeleteAccount(ctx context.Context, id uint64) (err error) {
	return s.m.DeleteAccount(ctx, id)
}

// SetDefaultAccount 切换某用途的默认账号（同用途唯一，先清后设在同一事务内）。
func (s *Service) SetDefaultAccount(ctx context.Context, id uint64) (err error) {
	e, err := s.m.GetAccount(ctx, id)
	if err != nil {
		return errors.New(mailenums.ErrAccountNotFound)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.ClearDefaultAccounts(ctx, tx, e.Purpose); cerr != nil {
			return cerr
		}
		return s.m.MarkAccountDefaultTx(ctx, tx, id)
	})
}

// TestSend 用指定账号发一封测试邮件，并把结论留在账号行上（后台「测试发送」按钮）。
//
// 这是唯一会真正建立 SMTP 连接的地方，也是运营验证配置对不对的入口：
// 失败原因按错误分类记进 last_check_error，界面直接能看出「认证失败」还是「连不上」。
func (s *Service) TestSend(ctx context.Context, req *maildto.TestSendReq) (res *maildto.TestSendResp, err error) {
	if req == nil || req.AccountID == 0 || strings.TrimSpace(req.ToEmail) == "" {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	sender, account, err := s.senderFor(ctx, req.AccountID)
	if err != nil {
		return nil, err
	}
	to := strings.TrimSpace(req.ToEmail)
	now := time.Now()
	_, sendErr := sender.Send(ctx, &mailer.Message{
		To:      []mailer.Address{{Email: to}},
		Subject: mailenums.TestMailSubject,
		HTML:    mailenums.TestMailHTML,
		Text:    mailenums.TestMailText,
	})
	res = &maildto.TestSendResp{To: to}
	if sendErr != nil {
		kind, msg := describeSendError(sendErr)
		res.OK = false
		res.ErrorKind = kind
		res.Error = msg
		_ = s.m.UpdateAccountFields(ctx, account.ID, map[string]any{
			"last_check_at":    now,
			"last_check_error": msg,
			"update_time":      now,
		})
		return res, nil
	}
	res.OK = true
	_ = s.m.UpdateAccountFields(ctx, account.ID, map[string]any{
		"last_check_at":    now,
		"last_check_error": nil,
		"update_time":      now,
	})
	return res, nil
}

// senderFor 由账号行构造发送器（解密只发生在这里）。
func (s *Service) senderFor(ctx context.Context, accountID uint64) (sender mailer.Sender, account *mailmodel.MailAccountEntity, err error) {
	account, err = s.m.GetAccount(ctx, accountID)
	if err != nil {
		return nil, nil, errors.New(mailenums.ErrAccountNotFound)
	}
	if account.Status != mailmodel.AccountStatusEnabled {
		return nil, nil, errors.New(mailenums.ErrAccountDisabled)
	}
	if account.Host == nil || account.Port == nil || strings.TrimSpace(account.FromEmail) == "" {
		return nil, nil, errors.New(mailenums.ErrAccountIncomplete)
	}

	password := ""
	if account.PasswordCipher != nil && *account.PasswordCipher != "" {
		plain, derr := crypto.Decrypt(*account.PasswordCipher, s.cipherSecret)
		if derr != nil {
			// 密钥缺失或密文损坏：给出可操作的提示，不要把底层错误原样抛给运营。
			return nil, nil, errors.New(mailenums.ErrCipherUnavailable)
		}
		password = plain
	}

	from := mailer.Address{Email: strings.TrimSpace(account.FromEmail)}
	if account.FromName != nil {
		from.Name = strings.TrimSpace(*account.FromName)
	}
	cfg := mailer.Config{
		Provider:   account.Provider,
		Host:       strings.TrimSpace(*account.Host),
		Port:       *account.Port,
		Encryption: account.Encryption,
		From:       from,
	}
	if account.Username != nil {
		cfg.Username = strings.TrimSpace(*account.Username)
	}
	cfg.Password = password

	sender, err = mailer.NewSender(cfg)
	if err != nil {
		return nil, nil, err
	}
	return sender, account, nil
}

// encryptPassword 加密 SMTP 密码（密钥未配置时明确报错）。
func (s *Service) encryptPassword(plain string) (string, error) {
	if strings.TrimSpace(s.cipherSecret) == "" {
		return "", errors.New(mailenums.ErrCipherSecretMissing)
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

// describeSendError 把发送错误翻成「分类 + 可读原因」（错误分类决定要不要重试）。
func describeSendError(err error) (kind string, msg string) {
	e := mailer.AsError("smtp", err)
	if e == nil {
		return "", ""
	}
	msg = e.Message
	if e.Err != nil && msg == "" {
		msg = e.Err.Error()
	}
	return string(e.Kind), msg
}

func accountItemOf(e *mailmodel.MailAccountEntity) *maildto.AccountItem {
	item := &maildto.AccountItem{
		ID:          e.ID,
		Name:        e.Name,
		Purpose:     e.Purpose,
		IsDefault:   e.IsDefault,
		FromEmail:   e.FromEmail,
		Provider:    e.Provider,
		Encryption:  e.Encryption,
		RatePerHour: e.RatePerHour,
		Status:      e.Status,
		// 注意：**没有密码字段**。密码只在落库时加密、发信时解密，从不回显。
		HasPassword: e.PasswordCipher != nil && *e.PasswordCipher != "",
		Incomplete:  e.Host == nil || e.Port == nil,
	}
	if e.FromName != nil {
		item.FromName = *e.FromName
	}
	if e.ReplyTo != nil {
		item.ReplyTo = *e.ReplyTo
	}
	if e.Host != nil {
		item.Host = *e.Host
	}
	if e.Port != nil {
		item.Port = *e.Port
	}
	if e.Username != nil {
		item.Username = *e.Username
	}
	if e.LastCheckAt != nil {
		item.LastCheckAt = e.LastCheckAt.Format(time.RFC3339)
	}
	if e.LastCheckError != nil {
		item.LastCheckError = *e.LastCheckError
	}
	return item
}

func validateAccountReq(req *maildto.SaveAccountReq, creating bool) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.FromEmail) == "" {
		return errors.New(mailenums.ErrInvalidParam)
	}
	if creating && strings.TrimSpace(req.Host) == "" {
		return errors.New(mailenums.ErrAccountIncomplete)
	}
	return nil
}

func normalizePurpose(p string) string {
	if strings.TrimSpace(p) == mailmodel.AccountPurposeMarketing {
		return mailmodel.AccountPurposeMarketing
	}
	return mailmodel.AccountPurposeTransactional
}

func normalizeProvider(p string) string {
	if v := strings.TrimSpace(p); v != "" {
		return v
	}
	return "smtp"
}

func normalizeEncryption(e string) string {
	switch strings.TrimSpace(e) {
	case mailer.EncryptionNone:
		return mailer.EncryptionNone
	case mailer.EncryptionSSL:
		return mailer.EncryptionSSL
	default:
		return mailer.EncryptionStartTLS
	}
}

func nullable(v string) any {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return nil
}
