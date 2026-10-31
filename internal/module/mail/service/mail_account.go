package mailservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/crypto"
	"go_wp/pkg/i18n"
	"go_wp/pkg/mailer"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

func (s *Service) CreateAccount(ctx context.Context, req *maildto.SaveAccountReq) (res *maildto.AccountItem, err error) {
	if req == nil {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
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

	if _, derr := s.m.DefaultAccount(ctx, purpose); errors.Is(derr, gorm.ErrRecordNotFound) {
		e.IsDefault = true
	}
	if err = s.m.CreateAccount(ctx, e); err != nil {
		return nil, err
	}
	return accountItemOf(e), nil
}

func (s *Service) UpdateAccount(ctx context.Context, req *maildto.SaveAccountReq) (res *maildto.AccountItem, err error) {
	if req == nil || req.ID == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	if err = validateAccountReq(req, false); err != nil {
		return nil, err
	}
	if _, err = s.m.GetAccount(ctx, req.ID); err != nil {
		return nil, mailenums.Err(mailenums.ErrAccountNotFound)
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

func (s *Service) ListAccountsPage(ctx context.Context, purpose string, page, limit int) (res []*maildto.AccountItem, total int64, current int, err error) {
	list, total, current, err := s.m.ListAccountsPage(ctx, purpose, false, page, limit)
	if err != nil {
		return nil, 0, 0, err
	}
	res = make([]*maildto.AccountItem, 0, len(list))
	for _, e := range list {
		res = append(res, accountItemOf(e))
	}
	return res, total, current, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id uint64) (err error) {
	return s.m.DeleteAccount(ctx, id)
}

func (s *Service) SetDefaultAccount(ctx context.Context, id uint64) (err error) {
	e, err := s.m.GetAccount(ctx, id)
	if err != nil {
		return mailenums.Err(mailenums.ErrAccountNotFound)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.ClearDefaultAccounts(ctx, tx, e.Purpose); cerr != nil {
			return cerr
		}
		return s.m.MarkAccountDefaultTx(ctx, tx, id)
	})
}

func (s *Service) TestSend(ctx context.Context, req *maildto.TestSendReq) (res *maildto.TestSendResp, err error) {
	if req == nil || req.AccountID == 0 || strings.TrimSpace(req.ToEmail) == "" {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	sender, account, err := s.senderFor(ctx, req.AccountID)
	if err != nil {
		return nil, err
	}
	to := strings.TrimSpace(req.ToEmail)
	now := time.Now()

	_, sendErr := sender.Send(ctx, &mailer.Message{
		To:      []mailer.Address{{Email: to}},
		Subject: i18n.Translate(mailenums.TestMailSubject, mailenums.TestMailSubjectFallback, req.Lang),
		HTML:    i18n.Translate(mailenums.TestMailHTMLKey, mailenums.TestMailHTMLFallback, req.Lang),
		Text:    i18n.Translate(mailenums.TestMailText, mailenums.TestMailTextFallback, req.Lang),
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

func (s *Service) senderFor(ctx context.Context, accountID uint64) (sender mailer.Sender, account *mailmodel.MailAccountEntity, err error) {
	account, err = s.m.GetAccount(ctx, accountID)
	if err != nil {
		return nil, nil, mailenums.Err(mailenums.ErrAccountNotFound)
	}
	if account.Status != mailmodel.AccountStatusEnabled {
		return nil, nil, mailenums.Err(mailenums.ErrAccountDisabled)
	}
	if account.Host == nil || account.Port == nil || strings.TrimSpace(account.FromEmail) == "" {
		return nil, nil, mailenums.Err(mailenums.ErrAccountIncomplete)
	}

	password := ""
	if account.PasswordCipher != nil && *account.PasswordCipher != "" {
		plain, derr := crypto.Decrypt(*account.PasswordCipher, s.cipherSecret)
		if derr != nil {

			return nil, nil, mailenums.Err(mailenums.ErrCipherUnavailable)
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

func (s *Service) encryptPassword(plain string) (string, error) {
	if strings.TrimSpace(s.cipherSecret) == "" {
		return "", mailenums.Err(mailenums.ErrCipherSecretMissing)
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

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
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	if creating && strings.TrimSpace(req.Host) == "" {
		return mailenums.Err(mailenums.ErrAccountIncomplete)
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
