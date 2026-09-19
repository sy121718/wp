// Package pagecontract 定义 page 模块对外能力。
package pagecontract

import (
	"context"
	"encoding/json"
	"errors"

	pagedto "go_wp/internal/module/page/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import page/dto。
type (
	CreateReq     = pagedto.CreateReq
	SaveDraftReq  = pagedto.SaveDraftReq
	ListReq       = pagedto.ListReq
	DetailReq     = pagedto.DetailReq
	RevisionReq   = pagedto.RevisionReq
	RevisionResp  = pagedto.RevisionResp
	DeleteReq     = pagedto.DeleteReq
	PageResp      = pagedto.PageResp
	PageDraftResp = pagedto.PageDraftResp
	BuildReq      = pagedto.BuildReq
	PublishReq    = pagedto.PublishReq
	RollbackReq   = pagedto.RollbackReq
	UpdateURLReq  = pagedto.UpdateURLReq
	PublishResp   = pagedto.PublishResp

	// 系统页面槽位（BIZ-1）。
	SiteSlotListReq   = pagedto.SiteSlotListReq
	SiteSlotBindReq   = pagedto.SiteSlotBindReq
	SiteSlotUnbindReq = pagedto.SiteSlotUnbindReq
	SiteSlotListResp  = pagedto.SiteSlotListResp
	SiteSlotItem      = pagedto.SiteSlotItem

	// 待重建页面的只读反查（反查面）。跨模块调用方只依赖 contract，不直接 import page/dto。
	StalePageListReq   = pagedto.StalePageListReq
	StalePageListResp  = pagedto.StalePageListResp
	StalePageResp      = pagedto.StalePageResp
	StaleImpactSummary = pagedto.StaleImpactSummary

	// 重定向管理（审计 SEO-025）。
	RedirectListReq       = pagedto.RedirectListReq
	RedirectListResp      = pagedto.RedirectListResp
	RedirectItem          = pagedto.RedirectItem
	RedirectCreateReq     = pagedto.RedirectCreateReq
	RedirectDeleteReq     = pagedto.RedirectDeleteReq
	RedirectMergeReq      = pagedto.RedirectMergeReq
	RedirectProjectOption = pagedto.RedirectProjectOption
)

// SitePageResolver 系统页面槽位解析能力（构建期与片段层消费的**只读**面）。
//
// 单独成接口而不是直接把 PageService 递出去：消费方只需要「槽位 → 当前语言线上路径」
// 这一条读能力，拿到整个 PageService 等于把发布、删除、改 URL 也一起给了 —— 越权防护靠接口形状。
type SitePageResolver interface {
	// ResolveSitePages 返回「槽位 → 已发布路径」，只含已绑且已发布的槽位。
	// 未绑定或未发布一律不出现（调用方据此不输出链接，而不是猜一个默认值）。
	ResolveSitePages(ctx context.Context, projectID, lang string) (map[string]string, error)
}

// 预览编译错误哨兵：workbench 预览复用本契约的编译能力时，
// 经 errors.Is 精确分类 HTTP 状态码（解析失败 400 / 编译失败 422 / 其余 500），
// 文案由调用方（workbench enums）自行下发，此处仅作错误类型标识。
var (
	// ErrPreviewInvalidDocument 预览文档解析失败（JSON 非法或空文档）。
	ErrPreviewInvalidDocument = errors.New("预览文档解析失败")
	// ErrPreviewCompileFailed 预览编译失败。
	ErrPreviewCompileFailed = errors.New("预览编译失败")
)

// BuildQueueEnqueuer 构建队列的入队端口（审计 DB-007）。
//
// 由 build 模块实现、装配期注入。接口定义在**本模块**（而不是反向 import build 的契约）：
// page 只需要「有人能把超限的重建任务接走」，不必知道队列是谁、存在哪里。
// 依赖方向因此是 build → page（build 用 page 的构建能力做执行器），不会成环。
//
// 未注入时保持既有行为：超出单次上限的页面保持 stale 并记一条告警（不静默丢弃）。
type BuildQueueEnqueuer interface {
	EnqueuePageBuild(ctx context.Context, pageID string, draftVersion int64, buildInputHash string) error
}

// PageService 手工 Page 草稿、修订与发布管理能力。
type PageService interface {
	// SitePageResolver 槽位解析（构建期与片段层经这个只读面取「结算页在哪」）。
	SitePageResolver

	Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error)
	// List 列出页面摘要（必须带 projectID；themeID 可选过滤主题）。
	List(ctx context.Context, req *pagedto.ListReq) (res []pagedto.PageResp, err error)
	Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error)
	// ProjectOfPage 按页面 id 返回所属工程 id。
	//
	// 后台入口需要它：画布 / 历史 / 译文这些路由手上只有 pageId，而 Detail /
	// SaveDraft 等把 projectID 当作**必填的越权防护 scope**（少它只会得到
	// 「参数缺失」，看起来像「页面不存在」）。这是只读收窄方法 —— 只回答
	// 「这个页面属于谁」，不返回页面内容、也没有任何写能力。
	ProjectOfPage(ctx context.Context, pageID string) (projectID string, err error)
	// ListDrafts 列出全部未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描：
	// 跨页面复用提示与全站完成度分母需要 (source_hash, context) 的全站视图）。
	ListDrafts(ctx context.Context) (res []pagedto.PageDraftResp, err error)
	SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error)
	ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error)

	// CompilePreview 基于未落盘文档 JSON 编译完整 HTML（预览专用：不落盘、不影响产物）。
	// 复用正式构建同源编译管线；错误经 errors.Is 分类：
	// ErrPreviewInvalidDocument（解析失败）/ ErrPreviewCompileFailed（编译失败）/ 其余为内部错误。
	// projectID 为页面所属站点工程（驱动导航等站点级资源解析，与正式构建一致）；
	// currentPath 为页面逻辑访问路径（导航当前项高亮，块预览传空）；
	// lang 为预览目标语言（空 = 站点默认语言，多语言 P2）。
	CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string) (html []byte, err error)
	// Build 基于当前草稿构建并暂存产物（不激活线上）。
	Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error)
	// Publish 激活暂存产物。
	Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error)
	// Rollback 秒级回滚到历史产物。
	Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error)
	// UpdateURL 修改访问路径，旧路径按策略 301 或取消激活。
	UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error)

	// ListSiteSlots 列出系统页面槽位及其绑定状态（含**未绑定**的槽位，后台要一眼看全）。
	ListSiteSlots(ctx context.Context, req *pagedto.SiteSlotListReq) (res *pagedto.SiteSlotListResp, err error)
	// BindSiteSlot 把槽位绑到页面（校验页面存在、属本工程、未删除），并标记该工程页面待重建。
	BindSiteSlot(ctx context.Context, req *pagedto.SiteSlotBindReq) (err error)
	// UnbindSiteSlot 解绑槽位（幂等），并标记该工程页面待重建。
	UnbindSiteSlot(ctx context.Context, req *pagedto.SiteSlotUnbindReq) (err error)
	// SiteSlotRefsOfPage 列出引用了某页面的槽位键（页面删除前的引用检查）。
	SiteSlotRefsOfPage(ctx context.Context, pageID string) (slots []string, err error)
	// RebuildArtifact 按产物元数据里冻结的 source_document 重建丢失的产物文件
	// （灾难恢复：只重建文件，不激活、不改 DB 指针）。重建后调用方须比对
	// HashMatched：只有构建输入（源文档 + 组件注册表 + 编译期依赖）全部未变，
	// 才能拿回同一个 hash。
	RebuildArtifact(ctx context.Context, req *pagedto.RebuildArtifactReq) (res *pagedto.RebuildArtifactResp, err error)
	// GarbageCollectArtifacts 回收超出保留窗口且不再被任何指针引用的产物文件。
	// 保护集合 = 页面活跃/暂存指针 + 每语言激活暂存 + 路由指向；同 hash 被其他行
	// 引用时只标记 gc_pending 不删文件。默认 dryRun=true（必须显式传 false 才真删）。
	GarbageCollectArtifacts(ctx context.Context, req *pagedto.GCArtifactsReq) (res *pagedto.GCArtifactsResp, err error)
	// PurgeRetention 执行一次保留期清理：历史快照收敛（分批删）+ 超期产物回收。
	// 返回删除的历史快照行数（产物回收量在 GC 的响应里）。定时任务与后台手动触发共用
	// 这一个入口 —— 保留期只在一处定义、在一处执行（IDX-004 / IDX-005 / IDX-019）。
	PurgeRetention(ctx context.Context) (deletedRevisions int64, err error)
	// AuditPublication 巡检激活面：返回全部悬空/异常链接。
	// /site 直接服务文件系统，产物被误删时 DB 侧毫无察觉，本方法是唯一发现手段。
	AuditPublication(ctx context.Context) (res *pagedto.PublicationAuditResp, err error)
	// Delete 软删页面（deleted_at 置时间，审计留痕）并释放其全部路径占用
	// （reserved/active/redirect），同路径可被新页面重新占用。
	Delete(ctx context.Context, req *pagedto.DeleteReq) (err error)
	// RefreshThemeForTheme 把主题设置批量合入挂在该主题下全部页面（主题设置保存后调用）。
	RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error
	// RefreshStructureForTheme 把主题的页眉/页脚块绑定批量合入挂在该主题下
	// 全部页面的 settings.structure（主题换绑全局块后调用）。
	RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error
	// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用）。
	MarkStaleForTheme(ctx context.Context, themeID string) error
	// MarkStaleForBlock 把文档中经 core.globalref 引用或 settings.structure 页眉/页脚
	// 自选绑定该块的页面标记为待重建（块内容变更后调用，与 MarkStaleForTheme 互补）。
	MarkStaleForBlock(ctx context.Context, blockID string) error
	// MarkStaleByRegistryVersion 把「产物由旧组件产出」的页面标记为待重建。
	//
	// 触发时机：服务启动时。组件编译进二进制，部署新组件后没有运行时事件能提示
	// 「已有产物过期」，只能靠产物元数据里的 registry_version 指纹比对。
	// 只标记不重建；current 为空时不做任何标记。返回被标记的页面 ID。
	MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error)
	// MarkStaleForI18n 把全部页面标记为待重建（界面文案词条变更后调用，
	// 与 Manifest 的 i18n 依赖条目配套，docs/06-D §10.4）。
	MarkStaleForI18n(ctx context.Context) error
	// MarkStaleByDependency 按依赖源 (kind,key) 精确标记受影响页面待重建，
	// 返回受影响的页面 ID（PIPE-3 fan-out：只命中活跃/暂存产物确实声明了该依赖
	// 的页面，与上面的全站标记严格区分）。
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
	// RebuildStale 重建指定页面（依赖失效后的自动重建端口，PIPE-3）：
	// 按站点启用语言逐个构建（只产生 staged Artifact），此前已发布的语言自动发布。
	RebuildStale(ctx context.Context, ids []string) error
	// CountBlockReference 统计引用该块的未删除页面数（globalref / structure 自选绑定），
	// 供 block 模块删除或切换 global→template 前的引用拦截（docs/02-D §9）。
	CountBlockReference(ctx context.Context, blockID string) (int64, error)
	// ListStalePages 只读反查：列出待重建页面（ProjectID 为空 = 全部工程），
	// 带完整计数与截断标记。limit 与排序由调用方给出（方法内不写死业务口径）。
	//
	// 存在的理由：pages.stale 此前只是一个布尔列 —— 后台能看见「有多少页待重建」，
	// 看不见「是哪些页、最近被谁标记」。本方法把「这次改动影响哪几个页面」变成
	// 一个可以直接读出来的投影（按标记时间倒序时，最近被标记的排在最前）。
	ListStalePages(ctx context.Context, req *pagedto.StalePageListReq) (res *pagedto.StalePageListResp, err error)
	// AttachThemeToUnassigned 把工程内未挂主题的页面挂到指定主题（工程首个主题创建后回填历史页面）。
	AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error
	// ReattachProjectPagesToTheme 把工程内全部页面（含已挂其他主题的）转挂到指定主题，
	// 用于切换激活主题后的「整站换皮」：使后续 RefreshThemeForTheme/RefreshStructureForTheme/
	// MarkStaleForTheme 以该主题为键命中全部页面。
	ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) error
	// ReskinProjectForTheme 激活主题后的整站换皮（转挂 + 刷新快照 + 标记 stale，同一事务）。
	ReskinProjectForTheme(ctx context.Context, projectID, themeID string, theme, structure json.RawMessage) error

	// ---- 重定向管理（审计 SEO-025）----
	//
	// 改 URL 留下的 301 此前只有「生效」这一半：产物与中间件都在，但没有界面能看见
	// 有哪些重定向、也无法手动增删。下面四条是管理页的完整能力面。

	// ListRedirects 列出工程下全部重定向（含未生效条目、多跳与成环标记）。
	ListRedirects(ctx context.Context, req *pagedto.RedirectListReq) (res *pagedto.RedirectListResp, err error)
	// CreateRedirect 手动新增重定向：源路径须空闲、目标须已激活、且不得成环。
	CreateRedirect(ctx context.Context, req *pagedto.RedirectCreateReq) (res *pagedto.RedirectItem, err error)
	// DeleteRedirect 删除一条重定向（解除访问面激活 + 清占用账）。
	DeleteRedirect(ctx context.Context, req *pagedto.RedirectDeleteReq) (err error)
	// MergeRedirectChain 把多跳链合并为直达（A→B、B→C 合成 A→C）。
	MergeRedirectChain(ctx context.Context, req *pagedto.RedirectMergeReq) (res *pagedto.RedirectItem, err error)
}
