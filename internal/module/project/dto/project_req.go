package projectdto

import json "go_wp/pkg/jsonx"

type CreateReq struct {
	Name     string          `json:"name" form:"name" binding:"required,max=200"`
	Settings json.RawMessage `json:"settings" form:"settings"`
}

type UpdateReq struct {
	ID       string          `json:"id" form:"id" binding:"required"`
	Name     string          `json:"name" form:"name" binding:"required,max=200"`
	Settings json.RawMessage `json:"settings" form:"settings"`
}

type DetailReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}
