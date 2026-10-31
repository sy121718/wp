package userdto

import (
	"net/url"
	"strconv"
	"strings"

	"go_wp/pkg/utils"
)

type RegisterReq struct {
	Username string `form:"username" json:"username"`
	Email    string `form:"email" json:"email"`
	Password string `form:"password" json:"password"`
	Nickname string `form:"nickname" json:"nickname"`

	RegisterIP       string `form:"-" json:"-"`
	RegisterLocation string `form:"-" json:"-"`

	Locale string `form:"-" json:"-"`
}

type ActivateEmailReq struct {
	Key string `form:"key" json:"key"`

	Locale string `form:"-" json:"-"`
}

type ResendActivationReq struct {
	Email  string `form:"email" json:"email"`
	Locale string `form:"-" json:"-"`
}

type PasswordResetReqRequest struct {
	Email  string `form:"email" json:"email"`
	Locale string `form:"-" json:"-"`
}

type ResetPasswordReq struct {
	Email       string `form:"email" json:"email"`
	Key         string `form:"key" json:"key"`
	NewPassword string `form:"password" json:"password"`
}

type LoginReq struct {
	Account  string `form:"account" json:"account"`
	Password string `form:"password" json:"password"`

	RememberMe bool `form:"-" json:"remember"`
}

func (r *LoginReq) BindForm(values url.Values) error {
	r.RememberMe = formBool(values, "remember")
	return nil
}

type LoginResp struct {
	Token     string
	UserID    uint64
	Username  string
	Nickname  string
	Avatar    string
	Email     string
	ExpiresAt utils.JSONTime
}

type SessionItem struct {
	ID           string
	UserAgent    string
	IP           string
	Location     string
	LastActiveAt *utils.JSONTime
	CreatedAt    *utils.JSONTime

	LastActiveText string
	CreatedText    string

	Current bool
}

type UpdateProfileReq struct {
	UserID    uint64 `form:"-" json:"-"`
	Nickname  string `form:"nickname" json:"nickname"`
	FirstName string `form:"firstName" json:"firstName"`
	LastName  string `form:"lastName" json:"lastName"`

	Gender   int    `form:"-" json:"gender"`
	Birthday string `form:"birthday" json:"birthday"`
	Bio      string `form:"bio" json:"bio"`
	Website  string `form:"website" json:"website"`
	Locale   string `form:"locale" json:"locale"`
	Timezone string `form:"timezone" json:"timezone"`
	Country  string `form:"country" json:"country"`
	Province string `form:"province" json:"province"`
	City     string `form:"city" json:"city"`
	Address  string `form:"address" json:"address"`
	Postcode string `form:"postcode" json:"postcode"`
	Phone    string `form:"phone" json:"phone"`
	Company  string `form:"company" json:"company"`
}

func (r *UpdateProfileReq) BindForm(values url.Values) error {
	r.Gender = formInt(values, "gender", 0)
	return nil
}

type UpdatePreferenceReq struct {
	UserID            uint64 `form:"-" json:"-"`
	Theme             string `form:"theme" json:"theme"`
	Locale            string `form:"locale" json:"locale"`
	Timezone          string `form:"timezone" json:"timezone"`
	PageSize          int    `form:"-" json:"pageSize"`
	EmailNotify       bool   `form:"-" json:"emailNotify"`
	SmsNotify         bool   `form:"-" json:"smsNotify"`
	ProfileVisibility string `form:"profileVisibility" json:"profileVisibility"`
	ShowOnline        bool   `form:"-" json:"showOnline"`
}

func (r *UpdatePreferenceReq) BindForm(values url.Values) error {
	r.PageSize = formInt(values, "pageSize", 20)
	r.EmailNotify = formBool(values, "emailNotify")
	r.SmsNotify = formBool(values, "smsNotify")
	r.ShowOnline = formBool(values, "showOnline")
	return nil
}

type ChangePasswordReq struct {
	UserID      uint64 `form:"-" json:"-"`
	OldPassword string `form:"oldPassword" json:"oldPassword"`
	NewPassword string `form:"newPassword" json:"newPassword"`
}

type ResetLinkReq struct {
	Email string `form:"email" json:"email"`
	Key   string `form:"key" json:"key"`
}

type RevokeSessionReq struct {
	ID string `form:"id" json:"id"`
}

func formText(values url.Values, key string) string {
	return strings.TrimSpace(values.Get(key))
}

func formBool(values url.Values, key string) bool {
	switch strings.ToLower(strings.TrimSpace(values.Get(key))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func formInt(values url.Values, key string, def int) int {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

type SiteRevokeReq struct {
	ID string `form:"id" json:"id"`
}
