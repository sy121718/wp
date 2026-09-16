package userdto

// customer_admin_resp.go — 后台「客户管理」的响应。
//
// **凭据零泄露**：这里只挑「运营要看的事实」，users 表上的 password /
// activation_key / activation_expires_at 一律不进来。少写一个字段，
// 比事后审计「哪个响应带上了密码」可靠得多 —— 后者永远漏一次。

import (
	"go_wp/pkg/utils"
)

// CustomerResp 客户一览（列表行与详情页共用同一份事实）。
type CustomerResp struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"displayName"`
	Avatar      string `json:"avatar"`
	Status      int    `json:"status"`
	// StatusLabel 状态的中文文案：状态值与文案必须同源，展示层不自己映射一遍。
	StatusLabel string `json:"statusLabel"`
	// EmailVerified 邮箱是否已验证（账号能不能自己登录，主要看这一条）。
	EmailVerified     bool            `json:"emailVerified"`
	RegisteredAt      *utils.JSONTime `json:"registeredAt"`
	RegisteredAtText  string          `json:"registeredAtText"`
	RegisterIP        string          `json:"registerIp"`
	RegisterLocation  string          `json:"registerLocation"`
	LastLoginTime     *utils.JSONTime `json:"lastLoginTime"`
	LastLoginTimeText string          `json:"lastLoginTimeText"`
	LastLoginIP       string          `json:"lastLoginIp"`
	LastLoginLocation string          `json:"lastLoginLocation"`
	// Locked 为真表示**此刻**处于登录锁定（locked_until_time 在未来）。
	// 它由 service 按当前时间判定：locked_until_time 非空不等于锁定 ——
	// 那个时间点过了之后值还在列里，拿「非空」当锁定会让页面永远显示「已锁定」。
	Locked            bool            `json:"locked"`
	LockedUntilTime   *utils.JSONTime `json:"lockedUntilTime"`
	LockedUntilText   string          `json:"lockedUntilText"`
	LoginFailureCount int             `json:"loginFailureCount"`
}

// CustomerListResp 客户列表结果。
type CustomerListResp struct {
	List  []*CustomerResp `json:"list"`
	Total int64           `json:"total"`
	// Counters 计数条（不受筛选影响：它回答的是「一共有多少账号、各是什么状态」）。
	Counters CustomerCounters `json:"counters"`
}

// CustomerCounters 客户账号的分布计数。
type CustomerCounters struct {
	Total      int64 `json:"total"`
	Active     int64 `json:"active"`
	Disabled   int64 `json:"disabled"`
	Pending    int64 `json:"pending"`
	Locked     int64 `json:"locked"`
	Verified   int64 `json:"verified"`
	Unverified int64 `json:"unverified"`
}

// CustomerStatusResp 状态写回执。
type CustomerStatusResp struct {
	CustomerID  uint64 `json:"customerId"`
	Status      int    `json:"status"`
	StatusLabel string `json:"statusLabel"`
}

// CustomerUnlockResp 解锁回执。
type CustomerUnlockResp struct {
	CustomerID uint64 `json:"customerId"`
	// Unlocked 为真表示这次调用解除了一个**生效中**的锁定。
	//
	// 如实回执而不是一律报「已解除」：运营点了个不会发生变化的按钮，
	// 需要知道的是「它本来就没锁」—— 否则下一次他还会再来点一遍。
	Unlocked bool `json:"unlocked"`
	// Cleared 为真表示这次调用清空了残留的登录失败计数（当时并未被锁定）。
	//
	// 与 Unlocked 分开：这两种结果对运营是两件事（「刚解锁」与「本来就没锁」），
	// 合成一个布尔就只能报其中一种，另一种永远显示错误。
	Cleared bool `json:"cleared"`
}
