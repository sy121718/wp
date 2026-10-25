package blockdto

import (
	json "go_wp/pkg/jsonx"
)

// CreateReq 新建全局块请求。
type CreateReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Kind      string          `json:"kind" form:"kind"`           // docs/02-D §4 类型白名单，默认 block
	Category  string          `json:"category" form:"category"`   // 自由分类（默认 general），白名单 ^[a-z0-9_-]{1,50}$
	ReuseMode string          `json:"reuseMode" form:"reuseMode"` // global（默认） | template，docs/02-D §5
	Document  json.RawMessage `json:"document" form:"document"`
}

// UpdateReq 更新全局块请求（编辑器整树保存）。
type UpdateReq struct {
	ID        string          `json:"id" form:"id" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Kind      string          `json:"kind" form:"kind"`
	Category  string          `json:"category" form:"category"`   // 自由分类（空则保留原值）
	ReuseMode string          `json:"reuseMode" form:"reuseMode"` // 空 then 保留原值；global→template 需无引用（否则 ErrBlockInUse）
	Document  json.RawMessage `json:"document" form:"document"`
}

// SaveContentReq 工作台保存块内容（POST /admin/blocks/save-content）的请求体。
//
// 与 UpdateReq 的区别只在用途：工作台只改文档，名称留空表示沿用现值；
// ReturnURL 是保存后的站内回跳目标（菜单页新建面板块时带回来，普通块编辑为空）。
type SaveContentReq struct {
	ID        string          `json:"id" form:"id" binding:"required"`
	Name      string          `json:"name" form:"name"`
	Document  json.RawMessage `json:"document" form:"document"`
	ReturnURL string          `json:"returnUrl" form:"returnUrl"`
}

// DetailReq 按 ID 查询块请求。
type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

// ListReq 列出工程块请求（kind/category/reuseMode 可选过滤）。
type ListReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Kind      string `json:"kind" form:"kind"`
	Category  string `json:"category" form:"category"`
	ReuseMode string `json:"reuseMode" form:"reuseMode"`
}

// DeleteReq 删除块请求。
// global 块被引用时默认拒绝（ErrBlockInUse）；Force=true 强制删除（引用页面退化为无该块并标待重建）。
type DeleteReq struct {
	ID    string `json:"id" form:"id" binding:"required"`
	Force bool   `json:"force" form:"force"`
}

// BulkDeleteReq 批量删除块请求（POST /api/block/bulk-delete）。
//
// 只有 ids，**没有 force** —— 这是刻意的：批量删除沿用旧的页面批量语义（force 恒为 false），
// 被页面 / 主题引用的 global 块一律拒绝并逐条回报原因。强制删除仍然只有单条路径
// （DeleteReq.Force）那一个入口：它是「明确知道后果」的单个动作，不该被批量顺手带过去。
//
// ids 由客户端构造（勾选框），因此服务端必须自己去重与限量（pkg/bulkids.Normalize）——
// 客户端的上限只是快速反馈，不是安全边界。binding 上的 dive,required 让空串元素在绑定期就被拒。
type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids" binding:"required,min=1,dive,required"`
}

// CloneReq 复制块 AST 请求（编辑器「插入-复制」动作，docs/02-D §5.2）。
type CloneReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}
