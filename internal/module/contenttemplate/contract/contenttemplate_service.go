// Package contenttemplatecontract contenttemplate 模块对外契约（0-A2）。
package contenttemplatecontract

import (
	"context"
	"encoding/json"

	"go_wp/internal/module/contenttemplate/dto"
)

// ContentTemplateService 内容结构模板管理契约（docs/02-domain.md §2）。
// 与 Page Blueprint 的关键区别：模板参与每次构建（presentation 派生
// DocumentSnapshot 时经 ResolveTemplate 取当前版本 AST）。
type ContentTemplateService interface {
	// Create 创建模板（初始 version=1）。
	Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error)
	// Update 修改模板 → 产生新不可变版本（draft_version 递增）。
	Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error)
	// Get 按 ID 查询。
	Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error)
	// List 按类型列表。
	List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error)
	// ResolveTemplate 取 entityType 的当前激活模板版本（presentation 派生
	// DocumentSnapshot 的唯一入口；同类型无模板时返回错误）。
	ResolveTemplate(ctx context.Context, entityType string) (res *ResolvedTemplate, err error)
}

// ResolvedTemplate 已解析的模板版本（presentation 派生快照的输入）。
type ResolvedTemplate struct {
	// VersionID 模板版本 ID（快照记录 source_template_version_id）。
	VersionID string
	// Version 版本号。
	Version int64
	// EntityType 内容类型（product/article/category）。
	EntityType string
	// Document 模板 AST（json.RawMessage，含 binding 节点；presentation
	// 编译时经 ContentResolver 解析为字面量）。
	Document json.RawMessage
}
