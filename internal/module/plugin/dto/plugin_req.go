package plugindto

type ToggleReq struct {
	ID      string `form:"id" json:"id" binding:"required"`
	Enabled bool   `form:"enabled" json:"enabled"`
}

type UninstallReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}

type DetailReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}
