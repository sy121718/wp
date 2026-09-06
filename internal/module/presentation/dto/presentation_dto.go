// Package presentationdto presentation 模块请求/响应结构。
package presentationdto

import "encoding/json"

// CreateInstanceReq 创建自动发布实例。
type CreateInstanceReq struct {
	EntityType string `json:"entityType" binding:"required"`
	EntityID   string `json:"entityId" binding:"required"`
	URLPath    string `json:"urlPath" binding:"required"`
}

// RebuildReq 实体数据更新后重建。
type RebuildReq struct {
	EntityID string `json:"entityId" binding:"required"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// DeleteReq 删除实例。
type DeleteReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按类型列表。
type ListReq struct {
	EntityType string `form:"entityType"`
}

// InstanceResp 实例响应。
type InstanceResp struct {
	ID           string          `json:"id"`
	EntityType   string          `json:"entityType"`
	EntityID     string          `json:"entityId"`
	URLPath      string          `json:"urlPath"`
	Status       string          `json:"status"`
	ArtifactHash string          `json:"artifactHash,omitempty"`
	SnapshotID   string          `json:"snapshotId,omitempty"`
	Document     json.RawMessage `json:"document,omitempty"`
	UpdatedAt    string          `json:"updatedAt"`
}
