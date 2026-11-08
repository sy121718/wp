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

	// ProjectID 只用于回跳筛选（列表页按工程过滤）；激活本身以主题自身归属为准。
	ProjectID string `json:"projectId" form:"projectId"`
}
