// Package contenttemplatecontract contenttemplate 模块对外契约（0-A2）。
package contenttemplatecontract

import (
	"context"
	"encoding/json"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
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

// 结构模板类型（页眉 / 页脚）。
//
// 定义在契约而不是 model（与上面的角色常量同一理由）：它出现在**跨模块的判定**上 ——
// 工作台要用它决定「这套模板需不需要样例实体」，装配层要用它决定「哪些模板能进主题的
// 页眉 / 页脚下拉」。判定散给各调用方各写一份 switch 时，新增一种结构类型只会在漏改的
// 那一处静默失效（表现是「新结构类型进不了可视化编辑」而不是编译错误）。
const (
	// EntityTypeHeader 页眉结构模板类型。
	EntityTypeHeader = "header"
	// EntityTypeFooter 页脚结构模板类型。
	EntityTypeFooter = "footer"
)

// IsStructureTemplateType 是否为结构模板类型（页眉 / 页脚）。
//
// 为什么是独立白名单而不是走实体来源注册表：header / footer 不是内容实体，
// 没有字段来源。往注册表里塞一个假来源，换来的是「这个类型可以配字段绑定」的假许可
// （构建期解析不到数据 → 页眉里一片空白），比拒绝更坏。
func IsStructureTemplateType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case EntityTypeHeader, EntityTypeFooter:
		return true
	default:
		return false
	}
}

// ContentTemplateService 内容结构模板管理契约（docs/02-domain.md §2）。
// 与 Page Blueprint 的关键区别：模板参与每次构建（presentation 派生
// DocumentSnapshot 时经 ResolveTemplate 取当前版本 AST）。
type ContentTemplateService interface {
	// Create 创建模板（初始 version=1）。
	Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error)
	// Update 修改模板 → 产生新不可变版本（draft_version 递增）。
	Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error)
	// Delete 删除模板（连带它的全部历史版本与内容模板级组件版本锁定行）。
	//
	// 被自动发布实例（presentation_instances.template_id）引用的模板会被数据库外键拒绝，
	// 返回 ErrTemplateInUse：实例是用户数据，删模板不该顺手删掉它们。
	Delete(ctx context.Context, req *contenttemplatedto.DeleteReq) (err error)

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
	// Activate 切换该（工程, 类型）的生效模板：旧的置 false、目标置 true（同事务）。
	//
	// 多套存着、单套生效的切换动作；生效的那套换了意味着引用它的页面/实例产物过期，
	// 由依赖失效扇出重建（端口注入见装配）。
	Activate(ctx context.Context, req *contenttemplatedto.ActivateReq) (res *contenttemplatedto.TemplateResp, err error)
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

	// ListBlockSourceRefs 列出文档树引用了该块的内容模板（审计 ARCH-02：块被模板消费）。
	//
	// 覆盖模板草稿与全部历史版本：两者都是**可编辑源码**（版本是源码的不可变快照，
	// 不是编译产物），删块都会留下断裂引用。装配层把它与 page / presentation / block
	// 的同名方法合并成块删除保护的完整判据。
	//
	// 不复用 Impact 的扫描：那条走的是装配层注入的 TemplateImpactPort（扫页面与实例文档），
	// 方向是「谁引用了模板」；这里问的是「谁引用了块」，数据源是本模块自己的两张表，
	// 语义与方向都不同。
	ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)

	// Impact 列出工程内引用了各模板的页面与实例（影响面提示与删除保护共用一次扫描）。
	//
	// 为什么返回**全部模板**的引用而不是按模板查询：后台列表页一次要渲染 N 套模板的
	// 「引用 N 处」，逐个模板各扫一遍页面与实例文档就是 N 次全表扫描；一次扫描后在
	// 调用方聚合，代价与模板数量无关。
	//
	// Available=false 表示引用反查端口未装配（不是「没有引用」，调用方必须区分显示）。
	Impact(ctx context.Context, req *contenttemplatedto.ImpactReq) (res *contenttemplatedto.ImpactResp, err error)
}

// TemplateImpactPort 模板引用反查端口（消费者侧最窄接口）。
//
// 只表达「这个工程里谁引用了模板」这一件事：调用方（contenttemplate）不需要知道
// 页面文档与实例存在哪张表、怎么扫。实现由装配层提供（page 契约 + presentation 契约
// 两条只读面），端口定义在本模块 —— 依赖方向是 contenttemplate ← 装配，
// 而不是 contenttemplate → page/presentation 的数据访问包。
//
// 为什么端口不放在 contract 的具体实现里而留成接口：单测可以给一个内存实现，
// 不必起数据库；装配层也可以在不改本模块的前提下换数据来源。
type TemplateImpactPort interface {
	// ListTemplateReferences 列出工程内全部模板引用（页面 + 实例）。
	//
	// templateIDs 是本工程已知的模板 id 集合：扫描要把文档里的绑定 id 认出来，
	// 而「文档解析不了」时只能退回字符串粗判（模板 id 是 uuid，误命中概率极低）——
	// 粗判需要这份集合，故由调用方传入而不是在端口里反向查询模板表。
	//
	// unparsable 是「文档无法解析、引用关系只能粗判」的条数：调用方据此提示
	// 「影响面可能不完整」，而不是把它当成 0。
	ListTemplateReferences(ctx context.Context, projectID string, templateIDs []string) (
		refs []contenttemplatedto.TemplateReference, unparsable int, err error)
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
	// TemplateRole 模板角色（detail / archive，见上面的 TemplateRole* 常量）。
	//
	// 为什么消费方需要它：编译期要按角色决定「这一页讲哪个实体」——归档模板渲染的是
	// 实例实体**下面的内容列表**（分类页 → 该分类下的商品），列表组件据此把筛选值
	// 落到实例实体上（builder.WithArchiveEntity）。少了它，消费方只能回头再查一次
	// 模板行，或者按调用方传参猜 —— 两处都会与模板真源分叉。
	TemplateRole string
	// Document 模板 AST（json.RawMessage，含 binding 节点；presentation
	// 编译时经 ContentResolver 解析为字面量）。
	Document json.RawMessage
}
