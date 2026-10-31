package blueprintdto

import json "go_wp/pkg/jsonx"

type BlueprintResp struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     string          `json:"updatedAt"`
}
