// ai_token_dto.go — 对外访问令牌（PAT）的请求与响应体。
//
// 一条硬约束：**明文令牌只出现在创建响应里**（TokenCreateResp.Token）。
// 列表项（TokenItem）不含明文也不含哈希 —— 它要渲染进后台页面，
// 而「后台页面能看到的字段」与「能被 XSS / 截屏带走的字段」是同一个集合。
package aidto

import (
	"go_wp/pkg/utils"
)

// TokenItem 令牌列表项（**不含明文、不含哈希**）。时间字段用 utils.JSONTime（对外只到秒）。
type TokenItem struct {
	ID int64 `json:"id"`
	// Name 用途备注；UserID 归属账号。
	Name   string `json:"name"`
	UserID int64  `json:"userId"`
	// TokenPrefix 明文前若干位，仅供展示与排错。
	TokenPrefix string `json:"tokenPrefix"`
	// Scopes 权限点子集；Status 取值见 aienums.TokenStatus，StatusLabel 是它的中文标签。
	Scopes      []string `json:"scopes"`
	Status      int16    `json:"status"`
	StatusLabel string   `json:"statusLabel"`
	// ExpiresAt 为空表示不过期；LastUsedTime 为空表示从未使用。
	ExpiresAt    *utils.JSONTime `json:"expiresAt,omitempty"`
	LastUsedTime *utils.JSONTime `json:"lastUsedTime,omitempty"`
	RevokedTime  *utils.JSONTime `json:"revokedTime,omitempty"`
	CreateTime   utils.JSONTime  `json:"createTime"`
}

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

// TokenCreateResp 创建结果：明文只在这里出现一次，此后库里只有哈希。
type TokenCreateResp struct {
	// Token 明文，一次性展示。
	Token string    `json:"token"`
	Item  TokenItem `json:"item"`
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
