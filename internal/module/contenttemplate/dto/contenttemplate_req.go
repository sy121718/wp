package contenttemplatedto

import json "go_wp/pkg/jsonx"

type CreateReq struct {
	EntityType string `json:"entityType" form:"entityType" binding:"required"`
	Name       string `json:"name" form:"name" binding:"required"`

	TemplateRole  string          `json:"templateRole" form:"templateRole"`
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument" binding:"required"`

	ProjectID string `json:"projectId" form:"projectId"`
}

type UpdateReq struct {
	ID            string          `json:"id" form:"id" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument" binding:"required"`
}

type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

// DeleteForm 是删除端点的原生表单输入：id 表示单条，ids 表示批量，两者互斥。
//
// 空项与非法 UUID 不在这里过滤，由 service 整批拒绝，不静默跳过。
type DeleteForm struct {
	ID  string   `form:"id"`
	IDs []string `form:"ids"`
}

type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids"`
}

type ActivateReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

type GetReq struct {
	ID string `form:"id" binding:"required"`
}

type ListReq struct {
	EntityType string `form:"entityType"`

	ProjectID string `form:"project" json:"projectId"`
}

type ImpactReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
}
