package projectdto

import (
	"encoding/json"
	"time"
)

// ProjectResp 站点工程响应。
type ProjectResp struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
