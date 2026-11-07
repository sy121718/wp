package userdto

import (
	"go_wp/pkg/utils"
)

type CustomerResp struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"displayName"`
	Avatar      string `json:"avatar"`
	Status      int    `json:"status"`

	StatusLabel string `json:"statusLabel"`

	EmailVerified     bool            `json:"emailVerified"`
	RegisteredAt      *utils.JSONTime `json:"registeredAt"`
	RegisteredAtText  string          `json:"registeredAtText"`
	RegisterIP        string          `json:"registerIp"`
	RegisterLocation  string          `json:"registerLocation"`
	LastLoginTime     *utils.JSONTime `json:"lastLoginTime"`
	LastLoginTimeText string          `json:"lastLoginTimeText"`
	LastLoginIP       string          `json:"lastLoginIp"`
	LastLoginLocation string          `json:"lastLoginLocation"`

	Locked            bool            `json:"locked"`
	LockedUntilTime   *utils.JSONTime `json:"lockedUntilTime"`
	LockedUntilText   string          `json:"lockedUntilText"`
	LoginFailureCount int             `json:"loginFailureCount"`
}

type CustomerListResp struct {
	List  []*CustomerResp `json:"list"`
	Total int64           `json:"total"`

	Counters CustomerCounters `json:"counters"`
}

type CustomerCounters struct {
	Total      int64 `json:"total"`
	Active     int64 `json:"active"`
	Disabled   int64 `json:"disabled"`
	Pending    int64 `json:"pending"`
	Locked     int64 `json:"locked"`
	Verified   int64 `json:"verified"`
	Unverified int64 `json:"unverified"`
}

type CustomerStatusResp struct {
	CustomerID uint64 `json:"customerId"`
	Status     int    `json:"status"`

	StatusLabel string `json:"statusLabel"`

	// MsgKey 是本次操作对应的消息事实 key；控制器只把它交给共享成功出口。
	MsgKey string `json:"-"`
}

type CustomerUnlockResp struct {
	CustomerID uint64 `json:"customerId"`

	Unlocked bool `json:"unlocked"`

	Cleared bool `json:"cleared"`

	// MsgKey 是本次操作对应的消息事实 key；控制器只把它交给共享成功出口。
	MsgKey string `json:"-"`
}
