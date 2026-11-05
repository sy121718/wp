package aidto

import (
	"go_wp/pkg/utils"
)

type TokenItem struct {
	ID int64 `json:"id"`

	Name   string `json:"name"`
	UserID int64  `json:"userId"`

	TokenPrefix string `json:"tokenPrefix"`

	Scopes []string `json:"scopes"`
	Status int16    `json:"status"`

	ExpiresAt    *utils.JSONTime `json:"expiresAt,omitempty"`
	LastUsedTime *utils.JSONTime `json:"lastUsedTime,omitempty"`
	RevokedTime  *utils.JSONTime `json:"revokedTime,omitempty"`
	CreateTime   utils.JSONTime  `json:"createTime"`
}

type TokenCreateResp struct {
	Token string    `json:"token"`
	Item  TokenItem `json:"item"`
}
