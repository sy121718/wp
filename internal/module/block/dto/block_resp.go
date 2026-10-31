package blockdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type BulkDeleteItemResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

type BulkDeleteResp struct {
	Deleted int                    `json:"deleted"`
	Skipped int                    `json:"skipped"`
	Items   []BulkDeleteItemResult `json:"items"`
}

type CloneResp struct {
	Document json.RawMessage `json:"document"`
}

type BlockResp struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Category  string          `json:"category"`
	ReuseMode string          `json:"reuseMode"`
	Document  json.RawMessage `json:"document"`
	CreatedAt utils.JSONTime  `json:"createdAt"`
	UpdatedAt utils.JSONTime  `json:"updatedAt"`

	Version int64 `json:"version"`

	PresetKey     *string `json:"presetKey,omitempty"`
	PresetVersion *int    `json:"presetVersion,omitempty"`
}
