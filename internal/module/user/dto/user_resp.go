package userdto

import (
	"go_wp/pkg/utils"
)

type RegisterResp struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Status   int    `json:"status"`

	MailQueued bool `json:"mailQueued"`
}

type ActivateResp struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
}

type AccountResp struct {
	UserID            uint64          `json:"userId"`
	Username          string          `json:"username"`
	Email             string          `json:"email"`
	EmailVerified     bool            `json:"emailVerified"`
	Nickname          string          `json:"nickname"`
	DisplayName       string          `json:"displayName"`
	Avatar            string          `json:"avatar"`
	Status            int             `json:"status"`
	RegisteredAt      *utils.JSONTime `json:"registeredAt"`
	LastLoginTime     *utils.JSONTime `json:"lastLoginTime"`
	LastLoginIP       string          `json:"lastLoginIp"`
	LastLoginLocation string          `json:"lastLoginLocation"`

	ProfileLocale   string `json:"profileLocale"`
	ProfileTimezone string `json:"profileTimezone"`
	FirstName       string `json:"firstName"`
	LastName        string `json:"lastName"`
	Gender          int    `json:"gender"`
	Birthday        string `json:"birthday"`
	Bio             string `json:"bio"`
	Website         string `json:"website"`
	Country         string `json:"country"`
	Province        string `json:"province"`
	City            string `json:"city"`
	Address         string `json:"address"`
	Postcode        string `json:"postcode"`
	Phone           string `json:"phone"`
	Company         string `json:"company"`

	Theme             string `json:"theme"`
	Locale            string `json:"locale"`
	Timezone          string `json:"timezone"`
	PageSize          int    `json:"pageSize"`
	EmailNotify       bool   `json:"emailNotify"`
	SmsNotify         bool   `json:"smsNotify"`
	ProfileVisibility string `json:"profileVisibility"`
	ShowOnline        bool   `json:"showOnline"`
}
