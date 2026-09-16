package userservice

// user_account.go — 账号中心（issue #36）。
//
// 三件事：看自己的账号、改资料与偏好、改密码。
// 共同的一条原则：**userID 一律由调用方（inbound）从会话里取，绝不从请求参数取**。
// 从请求里取 id 的账号接口等于「谁都能改别人的资料」，而且它看起来完全正常。

import (
	"context"
	"errors"
	"strings"
	"time"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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
