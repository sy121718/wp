package blueprintdto

// Package blueprintdto blueprint 模块请求/响应结构。

import json "go_wp/pkg/jsonx"

// BlueprintResp Blueprint 响应。
type BlueprintResp struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     string          `json:"updatedAt"`
}
