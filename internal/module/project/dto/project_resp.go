package projectdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type ProjectResp struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt utils.JSONTime  `json:"createdAt"`
	UpdatedAt utils.JSONTime  `json:"updatedAt"`
}

type RetentionPolicyResp struct {
	ProjectID     string `json:"projectId"`
	RetentionDays int    `json:"retentionDays"`
}
