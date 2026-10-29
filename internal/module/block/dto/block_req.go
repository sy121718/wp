package blockdto

import (
	"net/url"
	"unicode/utf8"

	blockenums "go_wp/internal/module/block/enums"
	json "go_wp/pkg/jsonx"
)

// CreateReq 新建全局块请求。
type CreateReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Kind      string          `json:"kind" form:"kind"`           // docs/02-D §4 类型白名单，默认 block
	Category  string          `json:"category" form:"category"`   // 自由分类（默认 general），白名单 ^[a-z0-9_-]{1,50}$
	ReuseMode string          `json:"reuseMode" form:"reuseMode"` // global（默认） | template，docs/02-D §5
	Document  json.RawMessage `json:"document" form:"-" binding:"excluded_with=PresetKey"`
	// 业务预设只提交来源，文档由受控注册解析；不允许客户端覆盖预设AST。
	PresetKey     string `json:"presetKey,omitempty" form:"presetKey" binding:"required_with=PresetVersion,max=100"`
	PresetVersion int    `json:"presetVersion,omitempty" form:"presetVersion" binding:"required_with=PresetKey,excluded_without=PresetKey,omitempty,min=1,max=2147483647"`
}

// Validate 校验预设来源与文档互斥；service直连、MCP同样执行此请求约束。
func (r *CreateReq) Validate() error {
	if r.PresetKey == "" {
		if r.PresetVersion == 0 {
			return nil
		}
	} else if utf8.RuneCountInString(r.PresetKey) <= 100 && r.PresetVersion > 0 && r.PresetVersion <= 2147483647 && len(r.Document) == 0 {
		return nil
	}
	return blockenums.Err(blockenums.ErrBlockPresetInvalid)
}

// BindForm 补绑定RawMessage；JSON仍由binding.JSON按原生值绑定。
func (r *CreateReq) BindForm(values url.Values) error {
	if documents, present := values["document"]; present {
		if r.PresetKey != "" || len(documents) != 1 {
			return blockenums.Err(blockenums.ErrBlockPresetInvalid)
		}
		r.Document = json.RawMessage(documents[0])
	}
	return r.Validate()
}

// UpdateReq 更新全局块请求（编辑器整树保存）。
type UpdateReq struct {
	ID        string          `json:"id" form:"id" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Kind      string          `json:"kind" form:"kind"`
	Category  string          `json:"category" form:"category"`   // 自由分类（空则保留原值）
	ReuseMode string          `json:"reuseMode" form:"reuseMode"` // 空 then 保留原值；global→template 需无引用（否则 ErrBlockInUse）
	Document  json.RawMessage `json:"document" form:"document"`

	// ExpectedVersion 必须是调用方读取到的并发版本，缺失或0一律拒绝。
	ExpectedVersion uint64 `json:"expectedVersion" form:"expectedVersion" binding:"required,min=1,max=9223372036854775807"`
}

// SaveContentReq 工作台保存块内容（POST /admin/blocks/save-content）的请求体。
//
// 与 UpdateReq 的区别只在用途：工作台只改文档，名称留空表示沿用现值；
// 保存后的导航由工作台前端按受控meta决定，不是保存请求的一部分。
type SaveContentReq struct {
	ID       string          `json:"id" form:"id" binding:"required"`
	Name     string          `json:"name" form:"name"`
	Document json.RawMessage `json:"document" form:"document"`
	// ExpectedVersion 同 UpdateReq.ExpectedVersion，必须提供真实版本。
	ExpectedVersion uint64 `json:"expectedVersion" form:"expectedVersion" binding:"required,min=1,max=9223372036854775807"`
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
// global 块仍被引用时一律拒绝（ErrBlockInUse），先解除可定位的引用再删除。
type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

// BulkDeleteReq 批量删除块请求（POST /api/block/bulk-delete）。
//
// 被引用的 global 块与单条删除同样拒绝，不能绕过源码引用保护。
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
