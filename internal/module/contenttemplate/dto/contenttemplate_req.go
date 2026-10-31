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

type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids" binding:"required,min=1,dive,required"`
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
