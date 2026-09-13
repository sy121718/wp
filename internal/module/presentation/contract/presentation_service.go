// Package presentationcontract presentation 模块对外契约（0-A2）。
package presentationcontract

import (
	"context"

	"go_wp/internal/module/presentation/dto"
)

// PresentationService 内容实体自动发布实例契约（docs/02-domain.md §3）。
// 与手工 Page 共享 Publish Compiler + ArtifactStore + PublicationStore，
// 但管理路径由 CMS 实体驱动（创建/编辑/删除，不经 Visual Builder）。
type PresentationService interface {
	// CreateInstance 为内容实体创建自动发布实例：解析模板 → 派生快照 →
	// 编译 → 发布激活（urlPath 由实体 slug 推导）。
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	// Rebuild 实体数据更新（revision 变化）后重建：重解析模板+实体 →
	// 新快照 → 重新编译发布。
	//
	// req.TemplateID 非空 = 切换实例绑定的模板后重建（issue #14 验收 2/4：
	// 发布时可指定用哪套模板，切换后产物随之变化）；为空 = 沿用实例当前绑定
	// （**不**回落「同类型最新模板」，否则一次内容更新就会悄悄换掉模板）。
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
	// GetByEntity 按内容实体查询实例（读当前绑定的模板与发布状态）。
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	// PreviewInstance 发布前预览：按指定（或默认）模板渲染实体，返回渲染结果。
	//
	// 只读：不写快照/产物/指针、不激活 URL、不改动线上产物（issue #14 验收 3）。
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
	// UpdateURL 修改已发布实例的线上路径（改 URL）。
	//
	// 新路径构建激活后，旧路径按 req.WithRedirect 登记 301 永久重定向或
	// 直接取消激活（与手工页面 page.Service.UpdateURL 同一语义）。
	// 详情页 URL 因此不再需要「删实例再重建」才能改。
	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)
	// Get 按 ID 查询。
	Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error)
	// List 按类型列表。
	List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error)
	// Delete 删除实例（级联删快照 + 反激活 URL）。
	Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error)
	// MarkStaleByDependency 按依赖源 (kind,key) 精确标记受影响实例待重建，返回命中实例 id。
	//
	// 依赖失效端口（与 page 契约同名方法同义，实现 pipeline.DependencyTarget）：
	// 编排层在依赖源变化时把失效范围落到具体实例上，而不是全量重建。
	// 键的构造用 pipeline.DirectContentKey / ContentCollectionKey（与构建期登记逐字一致）。
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
}
