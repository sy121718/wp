package aidto

type TokenCreateReq struct {
	Name string `json:"name" form:"name"`

	Scopes []string `json:"scopes" form:"scopes"`

	ExpiresAt string `json:"expiresAt" form:"expiresAt"`
	UserID    int64  `json:"-" form:"-"`
}

type TokenRevokeReq struct {
	ID int64 `json:"id" form:"id"`
}

type TokenListReq struct {
	All    bool  `json:"all" form:"all"`
	Limit  int   `json:"limit" form:"limit"`
	UserID int64 `json:"-" form:"-"`
}
