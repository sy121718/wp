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

// TokenCreateResp 创建结果：明文只在这里出现一次，此后库里只有哈希。
type TokenCreateResp struct {
	// Token 明文，一次性展示。
	Token string    `json:"token"`
	Item  TokenItem `json:"item"`
}
