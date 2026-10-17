package contenttemplatedto

// Package contenttemplatedto contenttemplate 模块请求/响应结构。

import json "go_wp/pkg/jsonx"

// BulkDeleteItemResult 批量删除的一条结果；失败 message 是当前请求语言的受控成品文案。
type BulkDeleteItemResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

// BulkDeleteResp 批量删除结果，部分成功仍返回 200。
type BulkDeleteResp struct {
	Deleted int                    `json:"deleted"`
	Skipped int                    `json:"skipped"`
	Items   []BulkDeleteItemResult `json:"items"`
}

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
