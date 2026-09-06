package blockdto

import (
	"encoding/json"
	"time"
)

// CreateReq 新建全局块请求。
type CreateReq struct {
	ProjectID string          `json:"projectId" binding:"required"`
	Name      string          `json:"name" binding:"required,max=100"`
	Kind      string          `json:"kind"`     // block | header | footer，默认 block
	Category  string          `json:"category"` // 自由分类（默认 general），白名单 ^[a-z0-9_-]{1,50}$
	Document  json.RawMessage `json:"document"`
}

// UpdateReq 更新全局块请求（编辑器整树保存）。
type UpdateReq struct {
	ID       string          `json:"id" binding:"required"`
	Name     string          `json:"name" binding:"required,max=100"`
	Kind     string          `json:"kind"`
	Category string          `json:"category"` // 自由分类（空则保留原值）
	Document json.RawMessage `json:"document"`
}

// DetailReq 按 ID 查询块请求。
type DetailReq struct {
	ID string `json:"id" binding:"required"`
}

// ListReq 列出工程块请求（kind/category 可选过滤）。
type ListReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Kind      string `json:"kind"`
	Category  string `json:"category"`
}

// DeleteReq 删除块请求。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// BlockResp 全局块投影。
type BlockResp struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Category  string          `json:"category"`
	Document  json.RawMessage `json:"document"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
