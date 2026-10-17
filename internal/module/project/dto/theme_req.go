package projectdto

import (
	json "go_wp/pkg/jsonx"
)

// ThemeCreateReq 新建主题请求。
type ThemeCreateReq struct {
	ProjectID string          `json:"projectId" binding:"required"`
	Name      string          `json:"name" binding:"required,max=100"`
	Settings  json.RawMessage `json:"settings"`
}

// ThemeUpdateReq 更新主题请求。
type ThemeUpdateReq struct {
	ID       string          `json:"id" binding:"required"`
	Name     string          `json:"name" binding:"required,max=100"`
	Settings json.RawMessage `json:"settings"`
}

// ThemeActivateReq 激活主题请求。
type ThemeActivateReq struct {
	ID string `json:"id" binding:"required"`
}
