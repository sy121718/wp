package aidto

// TokenCreateReq 创建令牌。
//
// UserID 不接受调用方自报（json:"-" form:"-"）：归属账号由服务端从登录态取，
// 否则任何能建令牌的人都可把令牌挂到别人名下，审计立刻失去意义。
type TokenCreateReq struct {
	Name string `json:"name" form:"name"`
	// Scopes 权限点子集（字符串形式，如 "order:list"）；至少一个，且必须都是已登记的权限点。
	Scopes []string `json:"scopes" form:"scopes"`
	// ExpiresAt 过期日（yyyy-mm-dd，可空 = 不过期）。语义是**该日结束**（次日零点失效）。
	ExpiresAt string `json:"expiresAt" form:"expiresAt"`
	UserID    int64  `json:"-" form:"-"`
}

// TokenRevokeReq 撤销令牌（按 id，撤销不删行）。
type TokenRevokeReq struct {
	ID int64 `json:"id" form:"id"`
}

// TokenListReq 列表筛选。
//
// All=true 时看全站令牌（需要管理页权限）；否则只看自己的。
type TokenListReq struct {
	All    bool  `json:"all" form:"all"`
	Limit  int   `json:"limit" form:"limit"`
	UserID int64 `json:"-" form:"-"`
}
