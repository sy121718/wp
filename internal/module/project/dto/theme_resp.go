package projectdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

// ThemeResp 主题投影(含设置:colors/fontFamily/headerPageId/footerPageId)。
type ThemeResp struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Name      string          `json:"name"`
	Settings  json.RawMessage `json:"settings"`
	IsActive  bool            `json:"isActive"`
	CreatedAt utils.JSONTime  `json:"createdAt"`
	UpdatedAt utils.JSONTime  `json:"updatedAt"`
}
