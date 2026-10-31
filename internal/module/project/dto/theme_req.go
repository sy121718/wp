package projectdto

import (
	json "go_wp/pkg/jsonx"
)

type ThemeCreateReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Settings  json.RawMessage `json:"settings" form:"settings"`
}

type ThemeUpdateReq struct {
	ID       string          `json:"id" form:"id" binding:"required"`
	Name     string          `json:"name" form:"name" binding:"required,max=100"`
	Settings json.RawMessage `json:"settings" form:"settings"`
}

type ThemeActivateReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}
