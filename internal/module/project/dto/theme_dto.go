package projectdto

import (
	"encoding/json"
	"time"
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

// ThemeResp 主题投影(含设置:colors/fontFamily/headerPageId/footerPageId)。
type ThemeResp struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Name      string          `json:"name"`
	Settings  json.RawMessage `json:"settings"`
	IsActive  bool            `json:"isActive"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
