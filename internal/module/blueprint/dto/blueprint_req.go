package blueprintdto

import json "go_wp/pkg/jsonx"

type CreateReq struct {
	Name          string          `json:"name" form:"name" binding:"required"`
	Kind          string          `json:"kind" form:"kind" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument" binding:"required"`
}

type UpdateReq struct {
	ID            string          `json:"id" form:"id" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument" binding:"required"`
}

type PublishReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

type GetReq struct {
	ID string `form:"id" binding:"required"`
}

type ListReq struct {
	Kind string `form:"kind"`
}

type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}
