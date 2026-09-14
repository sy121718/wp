package blockdto

import (
	"encoding/json"
	"time"
)

// CreateReq 新建全局块请求。
type CreateReq struct {
	ProjectID string          `json:"projectId" binding:"required"`
	Name      string          `json:"name" binding:"required,max=100"`
	Kind      string          `json:"kind"`      // docs/02-D §4 类型白名单，默认 block
	Category  string          `json:"category"`  // 自由分类（默认 general），白名单 ^[a-z0-9_-]{1,50}$
	ReuseMode string          `json:"reuseMode"` // global（默认） | template，docs/02-D §5
	Document  json.RawMessage `json:"document"`
}

// UpdateReq 更新全局块请求（编辑器整树保存）。
type UpdateReq struct {
	ID        string          `json:"id" binding:"required"`
	Name      string          `json:"name" binding:"required,max=100"`
	Kind      string          `json:"kind"`
	Category  string          `json:"category"`  // 自由分类（空则保留原值）
	ReuseMode string          `json:"reuseMode"` // 空 then 保留原值；global→template 需无引用（否则 ErrBlockInUse）
	Document  json.RawMessage `json:"document"`
}

// DetailReq 按 ID 查询块请求。
type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

// ListReq 列出工程块请求（kind/category/reuseMode 可选过滤）。
type ListReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Kind      string `json:"kind"`
	Category  string `json:"category"`
	ReuseMode string `json:"reuseMode"`
}

// DeleteReq 删除块请求。
// global 块被引用时默认拒绝（ErrBlockInUse）；Force=true 强制删除（引用页面退化为无该块并标待重建）。
type DeleteReq struct {
	ID    string `json:"id" binding:"required"`
	Force bool   `json:"force"`
}

// CloneReq 复制块 AST 请求（编辑器「插入-复制」动作，docs/02-D §5.2）。
type CloneReq struct {
	ID string `json:"id" binding:"required"`
}

// CloneResp 复制产物：与源块脱钩的独立 AST（全部节点已重生成 ID），可直接并入页面文档。
type CloneResp struct {
	Document json.RawMessage `json:"document"`
}

// BlockResp 全局块投影。
type BlockResp struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Category  string          `json:"category"`
	ReuseMode string          `json:"reuseMode"`
	Document  json.RawMessage `json:"document"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
