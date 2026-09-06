// Package blueprintdto blueprint 模块请求/响应结构。
package blueprintdto

import "encoding/json"

// CreateReq 创建 Blueprint。
type CreateReq struct {
	Name          string          `json:"name" binding:"required"`
	Kind          string          `json:"kind" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
}

// UpdateReq 修改草稿（产生新不可变版本）。
type UpdateReq struct {
	ID            string          `json:"id" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
}

// PublishReq 发布（基于当前草稿生成不可变版本）。
type PublishReq struct {
	ID string `json:"id" binding:"required"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按 kind 列表。
type ListReq struct {
	Kind string `form:"kind"`
}

// DeleteReq 删除。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// BlueprintResp Blueprint 响应。
type BlueprintResp struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     string          `json:"updatedAt"`
}
