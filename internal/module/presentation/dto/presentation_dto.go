// Package presentationdto presentation 模块请求/响应结构。
package presentationdto

import "encoding/json"

// CreateInstanceReq 创建自动发布实例。
type CreateInstanceReq struct {
	EntityType string `json:"entityType" binding:"required"`
	EntityID   string `json:"entityId" binding:"required"`
	URLPath    string `json:"urlPath" binding:"required"`
	// ProjectID 实例所属站点工程（presentation_instances.project_id 为 NOT NULL 外键）。
	// 可空：缺省时经 project 契约解析（工程唯一时取该工程），否则报参数错误。
	ProjectID string `json:"projectId"`
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
//
// Status 由 active_artifact_id 指针推导（active / draft），不是表列。
type InstanceResp struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	EntityType   string          `json:"entityType"`
	EntityID     string          `json:"entityId"`
	URLPath      string          `json:"urlPath"`
	TemplateID   string          `json:"templateId"`
	Status       string          `json:"status"`
	Stale        bool            `json:"stale"`
	ArtifactID   string          `json:"artifactId,omitempty"`
	ArtifactHash string          `json:"artifactHash,omitempty"`
	SnapshotID   string          `json:"snapshotId,omitempty"`
	Document     json.RawMessage `json:"document,omitempty"`
	UpdatedAt    string          `json:"updatedAt"`
}
