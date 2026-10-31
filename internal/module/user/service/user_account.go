package userservice

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

	mailcontract "go_wp/internal/module/mail/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
)

const birthdayLayout = "2006-01-02"

const (
	minPageSize = 1
	maxPageSize = 200
)

const displayTimeLayout = "2006-01-02 15:04"

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Format(displayTimeLayout)
}

var allowedProfileVisibility = map[string]struct{}{
	"public":  {},
	"members": {},
	"private": {},
}

func (s *Service) GetAccount(ctx context.Context, userID uint64) (res *userdto.AccountResp, err error) {
	if userID == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	u, gerr := s.m.GetByID(ctx, userID)
	if gerr != nil {
		return nil, userenums.Err(userenums.ErrUserNotFound)
	}

	res = &userdto.AccountResp{
		UserID:            u.ID,
		Username:          u.Username,
		Email:             u.Email,
		EmailVerified:     u.EmailVerifiedAt != nil,
		DisplayName:       deref(u.DisplayName),
		Avatar:            upload.StorageURL(deref(u.Avatar)),
		Status:            u.Status,
		RegisteredAt:      utils.NewJSONTimePtr(u.RegisteredAt),
		LastLoginTime:     utils.NewJSONTimePtr(u.LastLoginTime),
		LastLoginIP:       deref(u.LastLoginIP),
		LastLoginLocation: deref(u.LastLoginLocation),
	}
	if u.Nickname != nil {
		res.Nickname = *u.Nickname
	}

	if s.pm != nil {
		p, perr := s.pm.GetByUserID(ctx, userID)
		if perr != nil {
			return nil, perr
		}
		if p != nil {
			res.ProfileLocale = deref(p.Locale)
			res.ProfileTimezone = deref(p.Timezone)
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

			res.PageSize = 20
			res.EmailNotify = true
			res.ShowOnline = true
			res.ProfileVisibility = "public"
		}
	}
	return res, nil
}

func (s *Service) UpdateProfile(ctx context.Context, req *userdto.UpdateProfileReq) (err error) {
	if req == nil || req.UserID == 0 {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	nickname := strings.TrimSpace(req.Nickname)
	if nickname != "" {
		if n := len([]rune(nickname)); n > 60 {
			return userenums.Err(userenums.ErrUsernameTooLong)
		}
	}

	var birthday *time.Time
	if b := strings.TrimSpace(req.Birthday); b != "" {
		t, perr := time.Parse(birthdayLayout, b)
		if perr != nil {
			return userenums.Err(userenums.ErrInvalidParam)
		}
		birthday = &t
	}

	now := time.Now()

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

func (s *Service) UpdatePreference(ctx context.Context, req *userdto.UpdatePreferenceReq) (err error) {
	if req == nil || req.UserID == 0 {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	if req.PageSize < minPageSize || req.PageSize > maxPageSize {
		return userenums.Err(userenums.ErrPageSizeInvalid)
	}
	vis := strings.TrimSpace(req.ProfileVisibility)
	if vis == "" {
		vis = "public"
	}
	if _, ok := allowedProfileVisibility[vis]; !ok {
		return userenums.Err(userenums.ErrProfileVisibilityInvalid)
	}
	if s.prefm == nil {
		return userenums.Err(userenums.ErrInvalidParam)
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

func (s *Service) ChangePassword(ctx context.Context, req *userdto.ChangePasswordReq) (err error) {
	if req == nil || req.UserID == 0 {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	if len(req.NewPassword) < minPasswordLen {
		return userenums.Err(userenums.ErrPasswordTooShort)
	}
	u, gerr := s.m.GetByID(ctx, req.UserID)
	if gerr != nil {
		return userenums.Err(userenums.ErrUserNotFound)
	}
	if strings.TrimSpace(u.Password) == "" {

		return userenums.Err(userenums.ErrPasswordLoginUnavailable)
	}
	if cerr := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.OldPassword)); cerr != nil {
		return userenums.Err(userenums.ErrOldPasswordWrong)
	}
	if req.OldPassword == req.NewPassword {
		return userenums.Err(userenums.ErrNewPasswordSame)
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

func (s *Service) AccountOf(ctx context.Context, userID uint64) (res *userdto.AccountResp, err error) {
	return s.GetAccount(ctx, userID)
}

func (s *Service) SessionsOf(ctx context.Context, userID uint64, currentToken string) (items []*userdto.SessionItem, err error) {
	return s.ListSessions(ctx, userID, currentToken)
}

const passwordResetTemplate = "password_reset"

func (s *Service) RequestPasswordReset(ctx context.Context, req *userdto.PasswordResetReqRequest) (err error) {
	if req == nil || strings.TrimSpace(req.Email) == "" {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	email := strings.TrimSpace(req.Email)
	e, ferr := s.m.GetByEmail(ctx, email)
	if ferr != nil || e.Status != usermodel.UserStatusActive {

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

	_ = sendErr
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, req *userdto.ResetPasswordReq) (err error) {
	if req == nil || strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Key) == "" {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	if len(req.NewPassword) < minPasswordLen {
		return userenums.Err(userenums.ErrPasswordTooShort)
	}
	e, ferr := s.m.GetByEmail(ctx, strings.TrimSpace(req.Email))
	if ferr != nil {
		return userenums.Err(userenums.ErrActivationInvalid)
	}

	if e.ActivationKey == nil || *e.ActivationKey != crypto.Sha256(strings.ToUpper(strings.TrimSpace(req.Key))) {
		return userenums.Err(userenums.ErrActivationInvalid)
	}
	if e.ActivationExpiresAt != nil && time.Now().After(*e.ActivationExpiresAt) {
		return userenums.Err(userenums.ErrActivationInvalid)
	}
	if e.Status != usermodel.UserStatusActive {
		return userenums.Err(userenums.ErrActivationInvalid)
	}
	hashed, herr := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if herr != nil {
		return herr
	}

	if err := s.RevokeAllSessions(ctx, e.ID); err != nil {
		return err
	}
	now := time.Now()
	if uerr := s.m.UpdateFields(ctx, e.ID, map[string]any{
		"password":              string(hashed),
		"activation_key":        nil,
		"activation_expires_at": nil,

		"login_failure_count": 0,
		"locked_until_time":   nil,
		"update_time":         now,
	}); uerr != nil {
		return uerr
	}
	return nil
}

const (
	guestAccountPasswordLen = 12

	guestUsernameMaxLen = 24

	guestAccountTemplate = "guest_account"
)

var _ usercontract.GuestAccountProvisioner = (*Service)(nil)

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

func (s *Service) EnsureGuestAccount(ctx context.Context, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
	res, err = s.ensureGuestAccountOn(ctx, nil, in)
	if err != nil {
		return nil, err
	}

	if res.PendingMail != nil {
		res.PasswordMailed = s.sendGuestAccountMailOf(ctx, res.PendingMail)
		res.PendingMail = nil
	}
	return res, nil
}

func (s *Service) EnsureGuestAccountTx(ctx context.Context, tx *gorm.DB, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
	return s.ensureGuestAccountOn(ctx, tx, in)
}

func (s *Service) SendGuestAccountMail(ctx context.Context, mail *usercontract.GuestAccountMail) error {
	if mail == nil {
		return nil
	}
	if s.sendGuestAccountMailOf(ctx, mail) {
		return nil
	}
	return errors.New("初始密码邮件未受理")
}

func (s *Service) ensureGuestAccountOn(ctx context.Context, tx *gorm.DB, in *usercontract.GuestAccountInput) (res *usercontract.GuestAccountResult, err error) {
	if in == nil {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	email := strings.TrimSpace(in.Email)
	if email == "" {
		return nil, userenums.Err(userenums.ErrEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return nil, userenums.Err(userenums.ErrEmailInvalid)
	}

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

		if again, aerr := s.findUserByEmail(ctx, tx, email); aerr == nil && again != nil && again.ID != 0 {
			return &usercontract.GuestAccountResult{
				UserID: again.ID, Username: again.Username, Created: false,
			}, nil
		}
		return nil, err
	}

	return &usercontract.GuestAccountResult{
		UserID: e.ID, Username: e.Username, Created: true,

		PendingMail: &usercontract.GuestAccountMail{
			Email: email, Name: displayName(e), Username: e.Username,
			Password: password, Locale: in.Locale,
		},
	}, nil
}

func (s *Service) findUserByEmail(ctx context.Context, tx *gorm.DB, email string) (*usermodel.UserEntity, error) {
	if tx != nil {
		return s.m.GetByEmailTx(ctx, tx, email)
	}
	return s.m.GetByEmail(ctx, email)
}

func (s *Service) createUser(ctx context.Context, tx *gorm.DB, e *usermodel.UserEntity) error {
	if tx != nil {
		return s.m.CreateTx(ctx, tx, e)
	}
	return s.m.Create(ctx, e)
}

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

	return fmt.Sprintf("%s_%d", base, time.Now().UnixNano()%1000000)
}

func (s *Service) usernameTaken(ctx context.Context, tx *gorm.DB, username string) bool {
	var err error
	if tx != nil {
		_, err = s.m.GetByUsernameTx(ctx, tx, username)
	} else {
		_, err = s.m.GetByUsername(ctx, username)
	}
	return err == nil
}

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

func newGuestPassword() (string, error) {
	return randomGuestToken(guestAccountPasswordLen)
}

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
	activationKeyLen = 8

	activationTTL = 30 * time.Minute

	minPasswordLen = 8

	registerVerifyTemplate = "register_verify"

	welcomeTemplate = "welcome"
)

func (s *Service) Register(ctx context.Context, req *userdto.RegisterReq) (res *userdto.RegisterResp, err error) {
	if req == nil {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	if err = validateRegister(username, email, req.Password); err != nil {
		return nil, err
	}

	exists, err := s.m.CountByExistence(ctx, username, email, 0)
	if err != nil {
		return nil, err
	}
	if exists > 0 {

		if _, eerr := s.m.GetByUsername(ctx, username); eerr == nil {
			return nil, userenums.Err(userenums.ErrUsernameTaken)
		}
		return nil, userenums.Err(userenums.ErrEmailTaken)
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

	res.MailQueued = s.sendVerifyMail(ctx, email, username, req.Nickname, plainKey, req.Locale)
	return res, nil
}

func (s *Service) ActivateEmail(ctx context.Context, req *userdto.ActivateEmailReq) (res *userdto.ActivateResp, err error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	key := strings.ToUpper(strings.TrimSpace(req.Key))
	e, err := s.m.GetByActivationKey(ctx, crypto.Sha256(key))
	if err != nil {
		return nil, userenums.Err(userenums.ErrActivationInvalid)
	}
	if e.ActivationExpiresAt != nil && time.Now().After(*e.ActivationExpiresAt) {
		return nil, userenums.Err(userenums.ErrActivationInvalid)
	}
	now := time.Now()
	if uerr := s.m.UpdateFields(ctx, e.ID, map[string]any{
		"status":            usermodel.UserStatusActive,
		"email_verified_at": now,

		"activation_key":        nil,
		"activation_expires_at": nil,
		"update_time":           now,
	}); uerr != nil {
		return nil, uerr
	}

	s.sendWelcomeMail(ctx, e.Email, displayName(e), req.Locale)
	return &userdto.ActivateResp{UserID: e.ID, Username: e.Username}, nil
}

func (s *Service) ResendActivation(ctx context.Context, req *userdto.ResendActivationReq) (err error) {
	if req == nil || strings.TrimSpace(req.Email) == "" {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	e, err := s.m.GetByEmail(ctx, strings.TrimSpace(req.Email))
	if err != nil {
		return userenums.Err(userenums.ErrUserNotFound)
	}
	if e.Status != usermodel.UserStatusPending {

		return userenums.Err(userenums.ErrActivationInvalid)
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
		return userenums.Err(userenums.ErrMailUnavailable)
	}
	return nil
}

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

func newActivationKey() (plain, hash string, err error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
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
		return userenums.Err(userenums.ErrUsernameRequired)
	}
	if len([]rune(username)) < 3 {
		return userenums.Err(userenums.ErrUsernameTooShort)
	}
	if len([]rune(username)) > 32 {
		return userenums.Err(userenums.ErrUsernameTooLong)
	}
	if email == "" {
		return userenums.Err(userenums.ErrEmailRequired)
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return userenums.Err(userenums.ErrEmailInvalid)
	}
	if len(password) < minPasswordLen {
		return userenums.Err(userenums.ErrPasswordTooShort)
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
