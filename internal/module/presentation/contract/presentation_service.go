// Package presentationcontract presentation 模块对外契约（0-A2）。
package presentationcontract

import (
	"context"

	blockcontract "go_wp/internal/module/block/contract"
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
	// SaveOverrideDocument 保存实例级文档覆盖并按其重建发布（迁移 281，docs/04-C-instance-override.md）：
	// workbench 实例模式的保存通道；只改本实例，不影响共享模板与同模板的其他实例。
	SaveOverrideDocument(ctx context.Context, req *presentationdto.SaveOverrideReq) (res *presentationdto.InstanceResp, err error)
	// ClearOverride 清除实例级文档覆盖并按（新）模板重建：放弃自定义 / 换底稿。
	ClearOverride(ctx context.Context, req *presentationdto.ClearOverrideReq) (res *presentationdto.InstanceResp, err error)
	// MarkStaleForI18n 把各工程内的全部自动发布实例标记为待重建：文案词条（sys_i18n）
	// 与内容译文（sys_translation）都由构建期取词注入 HTML 字节，一变就过期；
	// 触发源是后台翻译页/运维脚本（它们没有工程上下文，故由 service 逐工程扇出）。
	MarkStaleForI18n(ctx context.Context) error
	// Get 按 ID 查询。
	Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error)
	// List 按类型列表。
	List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error)
	// Delete 删除实例（级联删快照 + 反激活 URL）。
	Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error)
	// EnsureArchiveInstance 确保某实体的归档页存在（审计 EDT-004）：实体侧
	// （分类 / 标签 / 品牌）增删改时调用。未配置归档模板时返回 Skipped 而不是错误 ——
	// 「这个站点不要归档页」是正常状态，不该让新建分类变成一个会失败的操作。
	EnsureArchiveInstance(ctx context.Context, req *presentationdto.EnsureArchiveReq) (res *presentationdto.EnsureArchiveResp, err error)
	// MarkStaleByDependency 按依赖源 (kind,key) 精确标记受影响实例待重建，返回命中实例 id。
	//
	// 依赖失效端口（与 page 契约同名方法同义，实现 pipeline.DependencyTarget）：
	// 编排层在依赖源变化时把失效范围落到具体实例上，而不是全量重建。
	// 键的构造用 pipeline.DirectContentKey / ContentCollectionKey（与构建期登记逐字一致）。
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
	// ListBlockSourceRefs 列出文档树引用了该块的自动发布实例（审计 ARCH-02）。
	//
	// 覆盖实例级覆盖文档（独立文档模式）与全部文档快照：两者都是可编辑源码（或它的
	// 派生输入），删块会让下一次重建缺一段。**不含** presentation_artifacts 里的
	// 不可变产物 —— 那部分的保留由 GC 策略决定，不阻断源码删除。
	ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)
	// ListArtifactHashes 列出本模块认领的全部产物 hash（IDX-015 反向对账的属主清单）。
	// 只读且不含内容：对账只需要回答「这些磁盘目录是不是我们产出的」。
	ListArtifactHashes(ctx context.Context) (hashes []string, err error)
}

// ArchiveInstanceEnsurer 归档页的按需创建（审计 EDT-004）。
//
// 消费者（实体模块：分类 / 标签 / 品牌）只拿得到这一条方法：归档页该不该建、
// 路径怎么定、用哪套模板，全在 presentation 内部决定 —— 否则归档规则会散落到
// 每个实体模块里各写一份，改一次规则要改好几处。
type ArchiveInstanceEnsurer interface {
	EnsureArchiveInstance(ctx context.Context, req *presentationdto.EnsureArchiveReq) (res *presentationdto.EnsureArchiveResp, err error)
}

// BuildQueueEnqueuer 自动重建任务的入队端口（PERF-020）。
//
// 由 build 模块实现、装配期注入（方向 presentation ← build，与 page 契约里的
// 同名端口同模式）。自动重建改为入队而不是在触发进程里同步重建：进程内互斥锁
// 在多实例部署下拦不住两个实例同时重建同一实例，队列消费侧的 SKIP LOCKED claim
// 才是跨实例互斥的落点。
type BuildQueueEnqueuer interface {
	// EnqueuePresentationBuild 入队一条实例重建任务；同一实例同时只有一条待办
	// （队列侧部分唯一索引去重，重复入队是幂等的）。
	//
	// projectID 是任务的工程作用域，由本模块显式带过来（审计 DB-01）：
	// 逐工程扇出那条路径先按实例定位工程再传（列可空，定位不到就传空串），
	// 队列侧不反查来源表。
	EnqueuePresentationBuild(ctx context.Context, presentationID, projectID string) error
}
