package blockdto

import (
	"encoding/json"
	"go_wp/pkg/utils"
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

// BulkDeleteReq 批量删除块请求（POST /api/block/bulk-delete）。
//
// 只有 ids，**没有 force** —— 这是刻意的：批量删除沿用旧的页面批量语义（force 恒为 false），
// 被页面 / 主题引用的 global 块一律拒绝并逐条回报原因。强制删除仍然只有单条路径
// （DeleteReq.Force）那一个入口：它是「明确知道后果」的单个动作，不该被批量顺手带过去。
//
// ids 由客户端构造（勾选框），因此服务端必须自己去重与限量（pkg/bulkids.Normalize）——
// 客户端的上限只是快速反馈，不是安全边界。binding 上的 dive,required 让空串元素在绑定期就被拒。
type BulkDeleteReq struct {
	IDs []string `json:"ids" binding:"required,min=1,dive,required"`
}

// BulkDeleteItemResult 批量删除里的一条结果。
//
// Deleted=false 时 Message 是**已按请求语言取过词**的受控文案（含引用明细，如
// 「该区块仍被引用：页面 #42」）；成功时留空。为什么这一层回成品句而不是枚举 key：
// 消费方是本页前端，它没有取词能力，而机读的部分是 ID 与 Deleted 两个字段。
// （顶层 message 走 response 的取词链，仍是 key 协议 —— 这两条链别混。）
type BulkDeleteItemResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

// BulkDeleteResp 批量删除结果。
//
// 部分成功也是 200：整批回 4xx/5xx 会让前端把「删了 3 个、跳了 1 个」显示成「全部失败」，
// 用户于是反复重试已删掉的块。真实计数（Deleted / Skipped）与逐条结果（Items）才是结论。
type BulkDeleteResp struct {
	Deleted int                    `json:"deleted"`
	Skipped int                    `json:"skipped"`
	Items   []BulkDeleteItemResult `json:"items"`
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
	CreatedAt utils.JSONTime  `json:"createdAt"`
	UpdatedAt utils.JSONTime  `json:"updatedAt"`
}
