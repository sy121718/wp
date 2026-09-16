package projectdto

import (
	"encoding/json"
	"go_wp/pkg/utils"
)

// ProjectResp 站点工程响应。
type ProjectResp struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt utils.JSONTime  `json:"createdAt"`
	UpdatedAt utils.JSONTime  `json:"updatedAt"`
}
