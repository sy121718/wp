// Package contenttemplatedto contenttemplate 模块请求/响应结构。
package contenttemplatedto

import "encoding/json"

// CreateReq 创建模板。
type CreateReq struct {
	EntityType string `json:"entityType" binding:"required"`
	Name       string `json:"name" binding:"required"`
	// TemplateRole 模板角色（审计 EDT-004）：空 = detail（既有一切调用方不变），
	// archive = 归档列表页模板（如「分类页」：列该分类下的内容）。
	TemplateRole  string          `json:"templateRole"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
	// ProjectID 模板所属站点工程（content_templates.project_id 为 NOT NULL 外键）。
	// 可空：缺省时经 project 契约解析（工程唯一时取该工程），否则报参数错误。
	ProjectID string `json:"projectId"`
}

// UpdateReq 修改模板（产生新版本）。
type UpdateReq struct {
	ID            string          `json:"id" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
}

// DeleteReq 删除模板。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// ActivateReq 切换生效模板（多套存着、单套生效）。
//
// 同一（工程, 类型）下至多一套生效：部分唯一索引
// idx_content_templates_default_per_project_type 兜底并发。
type ActivateReq struct {
	ID string `json:"id" binding:"required"`
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
	ID         string `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
	// TemplateRole 模板角色（detail / archive）：前端区分详情类与归档类。
	TemplateRole string `json:"templateRole,omitempty"`
	// IsDefault 是否为该（工程, 类型）当前生效的那套（EDT-014）。
	IsDefault     bool            `json:"isDefault"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     string          `json:"updatedAt"`
}
