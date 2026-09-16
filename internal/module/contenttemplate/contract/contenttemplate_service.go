// Package contenttemplatecontract contenttemplate 模块对外契约（0-A2）。
package contenttemplatecontract

import (
	"context"
	"encoding/json"

	"go_wp/internal/module/contenttemplate/dto"
)

// 模板角色（审计 EDT-004）。
//
// 定义在契约而不是 model：它出现在 ResolveTemplateByRole 的参数位置上，属于跨模块
// 调用方需要知道的东西。留在 model 里会迫使 presentation 这类调用方 import 对方的
// 数据访问包 —— 与「跨模块只用 contract 与不可变 dto」的约定相悖（实测确有一处）。
const (
	// TemplateRoleDetail 实体详情页模板（既有语义，默认值）。
	TemplateRoleDetail = "detail"
	// TemplateRoleArchive 归档列表页模板（如「分类页」：列该分类下的内容）。
	TemplateRoleArchive = "archive"
)

// ContentTemplateService 内容结构模板管理契约（docs/02-domain.md §2）。
// 与 Page Blueprint 的关键区别：模板参与每次构建（presentation 派生
// DocumentSnapshot 时经 ResolveTemplate 取当前版本 AST）。
type ContentTemplateService interface {
	// Create 创建模板（初始 version=1）。
	Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error)
	// Update 修改模板 → 产生新不可变版本（draft_version 递增）。
	Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error)
	// Get 按 ID 查询（工程作用域取唯一工程；多工程部署用 GetScoped）。
	Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error)
	// GetScoped 在显式工程作用域内按 id 取模板（DB-009 第二批）。
	//
	// 存在的理由：content_templates 带 FORCE 策略，按 id 的读取必须告诉数据库
	// 「当前是哪个工程」。已经持有工程 id 的调用方（构建链路）走这条，
	// 不必依赖「工程唯一」这个前提。
	GetScoped(ctx context.Context, projectID, id string) (res *contenttemplatedto.TemplateResp, err error)
	// List 按类型列表。
	List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error)
	// ResolveTemplate 取 entityType 的当前激活模板版本（presentation 派生
	// DocumentSnapshot 的唯一入口；同类型无模板时返回错误）。
	ResolveTemplate(ctx context.Context, entityType string) (res *ResolvedTemplate, err error)
	// ResolveTemplateByID 按模板 ID 解析其**当前版本**（issue #14：同一实体类型下
	// 可有多套命名模板，发布与预览需按 ID 显式指定用哪一套；模板不存在时返回
	// ErrNotFound，不静默回落到类型默认模板）。
	ResolveTemplateByID(ctx context.Context, templateID string) (res *ResolvedTemplate, err error)
	// ResolveTemplateByRole 按实体类型与角色解析模板（审计 EDT-004）：
	// 归档型实例（分类页 / 标签页 / 品牌页）用 role=archive 取归档模板；
	// 该角色没有配置时返回 ErrNotFound，由调用方决定跳过还是报错。
	ResolveTemplateByRole(ctx context.Context, entityType, role string) (res *ResolvedTemplate, err error)

	// ---- 带显式工程作用域的解析入口（DB-009 第二批）----
	//
	// 为什么另开一组方法而不是给上面几个加参数：上面三个是 dashboard 的
	// 编译期依赖（后台页面直接引用该接口），改签名会连带动一片；
	// 而构建链路（presentation）手里本来就有工程 id —— 它需要的是
	// 「把 id 透下去」，不是「再解析一次唯一工程」。两组各自演进，互不绑架。

	// ResolveTemplateScoped 在显式工程作用域内解析该类型的当前模板版本。
	ResolveTemplateScoped(ctx context.Context, projectID, entityType string) (res *ResolvedTemplate, err error)
	// ResolveTemplateByRoleScoped 在显式工程作用域内按类型与角色解析模板。
	ResolveTemplateByRoleScoped(ctx context.Context, projectID, entityType, role string) (res *ResolvedTemplate, err error)
	// ResolveTemplateByIDScoped 在显式工程作用域内按模板 ID 解析当前版本。
	ResolveTemplateByIDScoped(ctx context.Context, projectID, templateID string) (res *ResolvedTemplate, err error)
}

// ResolvedTemplate 已解析的模板版本（presentation 派生快照的输入）。
type ResolvedTemplate struct {
	// TemplateID 模板 ID（presentation_instances.template_id 为 NOT NULL 外键
	// 指向 content_templates(id)，presentation 装配实例行时必须落库）。
	TemplateID string
	// VersionID 模板版本 ID（快照记录 source_template_version_id）。
	VersionID string
	// Version 版本号。
	Version int64
	// TemplateName 模板名（同一类型下多套命名模板的区分依据，issue #14）。
	TemplateName string
	// EntityType 内容类型（product/article/category）。
	EntityType string
	// Document 模板 AST（json.RawMessage，含 binding 节点；presentation
	// 编译时经 ContentResolver 解析为字面量）。
	Document json.RawMessage
}
