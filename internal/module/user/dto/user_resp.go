package userdto

// user_resp.go — 用户模块响应（issue #36）。

import (
	"go_wp/pkg/utils"
)

// RegisterResp 注册结果。
//
// **不返回激活码 / 激活链接**：那是用户邮箱里的东西。
// 接口回传它等于让任何调用方都能直接激活，验证邮箱这一步就白做了。
type RegisterResp struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Status   int    `json:"status"`
	// MailQueued 表示验证邮件已受理；false 时前端应提示「可点重发」。
	MailQueued bool `json:"mailQueued"`
}

// ActivateResp 邮箱验证结果。
type ActivateResp struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
}

// AccountResp 账号中心的一次性聚合视图（身份 + 资料 + 偏好）。
//
// 聚成一个结构体而不是三个接口：账号中心一屏就要这些，
// 拆成三个请求只会让页面出现「上半截已经显示、下半截还在转」的中间态。
type AccountResp struct {
	UserID        uint64          `json:"userId"`
	Username      string          `json:"username"`
	Email         string          `json:"email"`
	EmailVerified bool            `json:"emailVerified"`
	Nickname      string          `json:"nickname"`
	DisplayName   string          `json:"displayName"`
	Avatar        string          `json:"avatar"`
	Status        int             `json:"status"`
	RegisteredAt  *utils.JSONTime `json:"registeredAt"`
	LastLoginTime *utils.JSONTime `json:"lastLoginTime"`
	// RegisteredAtText / LastLoginTimeText 是给页面直接显示的时间文本（口径见 service.formatTime）。
	RegisteredAtText  string `json:"registeredAtText"`
	LastLoginTimeText string `json:"lastLoginTimeText"`
	LastLoginIP       string `json:"lastLoginIp"`
	LastLoginLocation string `json:"lastLoginLocation"`

	// 资料（user_profiles），未建行时为零值。
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Gender    int    `json:"gender"`
	Birthday  string `json:"birthday"`
	Bio       string `json:"bio"`
	Website   string `json:"website"`
	Country   string `json:"country"`
	Province  string `json:"province"`
	City      string `json:"city"`
	Address   string `json:"address"`
	Postcode  string `json:"postcode"`
	Phone     string `json:"phone"`
	Company   string `json:"company"`

	// 偏好（user_preferences）。
	Theme             string `json:"theme"`
	Locale            string `json:"locale"`
	Timezone          string `json:"timezone"`
	PageSize          int    `json:"pageSize"`
	EmailNotify       bool   `json:"emailNotify"`
	SmsNotify         bool   `json:"smsNotify"`
	ProfileVisibility string `json:"profileVisibility"`
	ShowOnline        bool   `json:"showOnline"`
}
