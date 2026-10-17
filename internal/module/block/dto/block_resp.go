package blockdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

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
