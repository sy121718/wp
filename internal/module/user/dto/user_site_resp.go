package userdto

import "go_wp/pkg/utils"

// SiteSessionResp 仅声明当前请求的访客登录事实，不包含任何会话凭据。
type SiteSessionResp struct {
	Authenticated bool `json:"authenticated"`
}

// SiteDevice 是面向当前访客的不透明设备标识与原始设备事实。
// ID 按当前登录令牌签名，不能作为 Redis 会话哈希使用。
type SiteDevice struct {
	ID           string          `json:"id"`
	UserAgent    string          `json:"userAgent"`
	IP           string          `json:"ip"`
	Location     string          `json:"location"`
	LastActiveAt *utils.JSONTime `json:"lastActiveAt"`
	CreatedAt    *utils.JSONTime `json:"createdAt"`
	Current      bool            `json:"current"`
}
