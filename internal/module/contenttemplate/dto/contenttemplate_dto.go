// Package contenttemplatedto contenttemplate 模块请求/响应结构。
package contenttemplatedto

import "encoding/json"

// CreateReq 创建模板。
type CreateReq struct {
	EntityType string `json:"entityType" binding:"required"`
	Name       string `json:"name" binding:"required"`
	// TemplateRole 模板角色（审计 EDT-004）：空 = detail（既有一切调用方不变），
	// archive = 归档列表页模板（如「分类页」：列该分类下的内容）。
	TemplateRole  string          `json:"templateRole"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
	// ProjectID 模板所属站点工程（content_templates.project_id 为 NOT NULL 外键）。
	// 可空：缺省时经 project 契约解析（工程唯一时取该工程），否则报参数错误。
	ProjectID string `json:"projectId"`
}

// UpdateReq 修改模板（产生新版本）。
type UpdateReq struct {
	ID            string          `json:"id" binding:"required"`
	DraftDocument json.RawMessage `json:"draftDocument" binding:"required"`
}

// DeleteReq 删除模板。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// ActivateReq 切换生效模板（多套存着、单套生效）。
//
// 同一（工程, 类型）下至多一套生效：部分唯一索引
// idx_content_templates_default_per_project_type 兜底并发。
type ActivateReq struct {
	ID string `json:"id" binding:"required"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按类型列表。
type ListReq struct {
	EntityType string `form:"entityType"`
	// ProjectID 工程作用域；空 = 唯一工程（多工程下报 ErrProjectRequired）。
	//
	// 后台页面（模板列表 / 主题设置的结构模板下拉）手里本来就有工程 id，走这条；
	// 空值保留旧行为，避免改动既有调用方。
	ProjectID string `form:"project" json:"projectId"`
}

// —— 引用反查（影响面提示与删除保护）——

// 引用来源类型（TemplateReference.Kind）。
const (
	// ReferenceKindPage 页面草稿文档的 settings.structure 绑定了该模板。
	ReferenceKindPage = "page"
	// ReferenceKindInstance 自动发布实例按该模板派生快照
	//（presentation_instances.template_id）或实例覆盖文档里绑定了该模板。
	ReferenceKindInstance = "instance"
)

// TemplateReference 一条「引用了某模板」的记录。
//
// 形状刻意做成**可定位**而不是一个计数：影响面提示与删除保护要回答的是
// 「是哪几张页面 / 哪几个实例」，只给数字的话运营还得自己去翻（那等于没给）。
// 两种来源各占一段字段，用 Kind 区分：页面看 Page*，实例看 Instance*。
type TemplateReference struct {
	// TemplateID 被引用的模板。
	TemplateID string `json:"templateId"`
	// Kind 引用来源：page / instance（取值见 ReferenceKind*）。
	Kind string `json:"kind"`
	// Slots 命中的结构槽位名（页面侧；面向运营的说法在 handler 层收敛）。
	Slots []string `json:"slots,omitempty"`

	// PageID / PagePath / PageTitle：Kind=page 时有效。
	PageID    string `json:"pageId,omitempty"`
	PagePath  string `json:"pagePath,omitempty"`
	PageTitle string `json:"pageTitle,omitempty"`

	// InstanceID / EntityType / EntityID / URLPath / RenderMode：Kind=instance 时有效。
	InstanceID string `json:"instanceId,omitempty"`
	EntityType string `json:"entityType,omitempty"`
	EntityID   string `json:"entityId,omitempty"`
	URLPath    string `json:"urlPath,omitempty"`
	RenderMode string `json:"renderMode,omitempty"`
}

// ImpactReq 引用反查请求（工程作用域）。
type ImpactReq struct {
	// ProjectID 工程 id；空时按「唯一工程」解析（多工程下报 ErrProjectRequired）。
	ProjectID string `json:"projectId" form:"projectId"`
}

// ImpactResp 引用反查结果。
//
// Available=false 不是「没有引用」，而是「没有能力回答」—— 两者在页面上必须显示成
// 不同的东西：前者是「可以放心处理」，后者是「别动，我查不出来」。
type ImpactResp struct {
	// Available 引用反查端口是否已装配。
	Available bool `json:"available"`
	// References 全部引用记录（一次扫描覆盖该工程的全部模板，供列表页整页渲染）。
	References []TemplateReference `json:"references"`
	// Unparsable 文档无法解析（引用关系不可判定）的条数：>0 时影响面可能不完整，
	// 页面要显式提示，而不是把它当成 0。
	Unparsable int `json:"unparsable"`
}

// TemplateResp 模板响应。
type TemplateResp struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
	// TemplateRole 模板角色（detail / archive）：前端区分详情类与归档类。
	TemplateRole string `json:"templateRole,omitempty"`
	// IsDefault 是否为该（工程, 类型）当前生效的那套（EDT-014）。
	IsDefault     bool            `json:"isDefault"`
	DraftVersion  int64           `json:"draftVersion"`
	DraftDocument json.RawMessage `json:"draftDocument"`
	UpdatedAt     string          `json:"updatedAt"`
}
