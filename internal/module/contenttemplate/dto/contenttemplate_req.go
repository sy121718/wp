package contenttemplatedto

// Package contenttemplatedto contenttemplate 模块请求/响应结构。

import json "go_wp/pkg/jsonx"

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

// BulkDeleteReq 批量删除模板请求。服务端负责去重、清空白与 200 条上限。
type BulkDeleteReq struct {
	IDs []string `json:"ids" binding:"required,min=1,dive,required"`
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
	// ProjectID 工程作用域；空 = 唯一工程（多工程下报 ErrProjectRequired）。
	//
	// 后台页面（模板列表 / 主题设置的结构模板下拉）手里本来就有工程 id，走这条；
	// 空值保留旧行为，避免改动既有调用方。
	ProjectID string `form:"project" json:"projectId"`
}

// —— 引用反查（影响面提示与删除保护）——

// ImpactReq 引用反查请求（工程作用域）。
type ImpactReq struct {
	// ProjectID 工程 id；空时按「唯一工程」解析（多工程下报 ErrProjectRequired）。
	ProjectID string `json:"projectId" form:"projectId"`
}
