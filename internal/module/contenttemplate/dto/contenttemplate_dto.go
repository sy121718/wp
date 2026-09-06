// Package contenttemplatedto contenttemplate 模块请求/响应结构。
package contenttemplatedto

import "encoding/json"

// CreateReq 创建模板。
type CreateReq struct {
	EntityType    string          `json:"entityType" binding:"required"`
	Name          string          `json:"name" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
}

// UpdateReq 修改模板（产生新版本）。
type UpdateReq struct {
	ID            string          `json:"id" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按类型列表。
type ListReq struct {
	EntityType string `form:"entityType"`
}

// TemplateResp 模板响应。
type TemplateResp struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	EntityType    string          `json:"entityType"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     string          `json:"updatedAt"`
}
