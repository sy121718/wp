package projectdto

import (
	json "go_wp/pkg/jsonx"
)

// ThemeCreateReq 新建主题请求。
type ThemeCreateReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Settings  json.RawMessage `json:"settings" form:"settings"`
}

// ThemeUpdateReq 更新主题请求。
type ThemeUpdateReq struct {
	ID       string          `json:"id" form:"id" binding:"required"`
	Name     string          `json:"name" form:"name" binding:"required,max=100"`
	Settings json.RawMessage `json:"settings" form:"settings"`
}

// ThemeActivateReq 激活主题请求。
type ThemeActivateReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}
