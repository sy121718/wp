// Package pagecontract 定义 page 模块对外能力。
package pagecontract

import (
	"context"
	"encoding/json"
	"errors"

	blockcontract "go_wp/internal/module/block/contract"
	pagedto "go_wp/internal/module/page/dto"
)

// ==========================================================================
// 跨模块形状：调用方要传进来、要收回去的类型
// ==========================================================================

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
	// PageLangState 页面级语言排除的状态投影（迁移 491），见 PageService.PageLangStates。
	PageLangState = pagedto.PageLangState
	// TranslationMissRow 缺译报告的一行（页面 × 语言），见 PageService.UntranslatedPageLangs。
	TranslationMissRow = pagedto.TranslationMissRow
	// PageTitleResp 页面标题投影（不含 draft_document），见 PageService.ListPageTitles。
	PageTitleResp = pagedto.PageTitleResp
	BuildReq      = pagedto.BuildReq
	PublishReq    = pagedto.PublishReq
	// PageBuildJobReq 构建队列任务的执行上下文（page 来源，审计 ARCH-04）。
	PageBuildJobReq = pagedto.PageBuildJobReq
	RollbackReq     = pagedto.RollbackReq
	UpdateURLReq    = pagedto.UpdateURLReq
	PublishResp     = pagedto.PublishResp

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

	// SEO 合规巡检（审计 SEO-01 前半段）。
	SEOPatrolReq      = pagedto.SEOPatrolReq
	SEOPatrolResp     = pagedto.SEOPatrolResp
	SEOArtifactItem   = pagedto.SEOArtifactItem
	SEOFindingItem    = pagedto.SEOFindingItem
	SEOUncheckedRoute = pagedto.SEOUncheckedRoute

	// 重定向管理（审计 SEO-025）。
	RedirectListReq       = pagedto.RedirectListReq
	RedirectListResp      = pagedto.RedirectListResp
	RedirectItem          = pagedto.RedirectItem
	RedirectCreateReq     = pagedto.RedirectCreateReq
	RedirectDeleteReq     = pagedto.RedirectDeleteReq
	RedirectMergeReq      = pagedto.RedirectMergeReq
	RedirectProjectOption = pagedto.RedirectProjectOption

	// 定时上下线（PIPE-7）。
	ScheduleSetReq    = pagedto.ScheduleSetReq
	ScheduleCancelReq = pagedto.ScheduleCancelReq
	ScheduleListReq   = pagedto.ScheduleListReq
	ScheduleItem      = pagedto.ScheduleItem
	ScheduleListResp  = pagedto.ScheduleListResp
	ScheduleRunResp   = pagedto.ScheduleRunResp
	// SchedulePageSummary 列表页的行内排定投影（每页一条待执行 + 一条最近失败）。
	SchedulePageSummary = pagedto.SchedulePageSummary
)

// page_preview_problem.go — 预览编译失败里「作者可操作」的那一类，用**类型**标记（而不是错误文本）。
//
// 背景：工作台画布的核心价值是「作者在画布上直接看到哪里坏了、怎么修」——例如
// 「手风琴至少需要一个折叠项（把组件拖入内部）」「常见问题至少需要一条」。这些提示由
// builder.ValidatePageTolerant 产出，而组件校验器返回的是**裸 fmt.Errorf**（不带类型）。
// page/service 过去用 `%w: %v` 把 compile 错误转成字符串，类型在链里就丢了，于是消费侧
// （workbench 的 422 出口）只有两条路：嗅探错误文本，或者把一切都压成一句泛化的
// 「预览编译失败」——两者都不能接受（前者判据不稳，后者把可操作信息丢给作者去猜）。
//
// 这个类型把「这条错误可以给作者看」变成**类型事实**：
//
//	产生处（page/service 的校验分支）—— 用 NewPreviewProblem 标记；
//	消费侧（workbench 的 422 出口）  —— 用 errors.As 判别，命中就把 Msg 透出到响应体。
//
// 反向同样重要：**不能给作者看的内部错误**（装配缺失、组件模板加载失败、驱动原文）
// 一律**不带**这个标记 —— 消费侧对它们只给归口文案 + 结构化日志，原文只进日志。
//
// 为什么放在 contract 而不是 page/service：消费侧（workbench）按模块边界不能 import
// page/service（见 internal/module/CLAUDE.md 的表隔离约定），而它必须能判别这个类型。
type PreviewProblem struct {
	// Msg 可展示的问题原文（组件校验器产出的、面向作者的提示，含节点定位）。
	Msg string
	// cause 由产生处传入（page/service 的 errCompileFailed 哨兵）：
	// 保证 errors.Is(err, errCompileFailed) 在包装之后**仍然成立** ——
	// 构建期与预览期既有的判别不能因为加了标记而变化。
	cause error
}

// NewPreviewProblem 构造一条可展示问题。
//
// msg 必须是**作者可操作**的提示（来自文档校验，不是驱动/装配原文）；
// cause 传调用方自己的编译失败哨兵（通常 errCompileFailed）。
func NewPreviewProblem(msg string, cause error) *PreviewProblem {
	return &PreviewProblem{Msg: msg, cause: cause}
}

// Error 实现 error：文本即可展示提示本身（不额外加前缀 —— 前缀「预览编译失败」
// 由消费侧按当前语言拼接，这里不做展示层的事）。
func (p *PreviewProblem) Error() string {
	if p == nil {
		return ""
	}
	return p.Msg
}

// Unwrap 返回 cause：让 errors.Is / errors.As 能继续往下穿（既有判别不受影响）。
func (p *PreviewProblem) Unwrap() error {
	if p == nil {
		return nil
	}
	return p.cause
}

// ==========================================================================
// 索要的端口：本模块需要外部给什么（由对方实现）
// ==========================================================================

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

// DependencyPageRef 按依赖键反查命中的页面（**最小**只读投影）。
//
// 只有两个字段：ID 用来定位，Title 用来给人看（pages 表没有标题列，
// 取的是文档 settings.seo.title；可能为空，读侧不替它编一个名字）。
//
// 刻意不带路径 / 状态 / 文档：带齐就成了第二个「页面列表」投影（PageResp 已有），
// 而反查面的调用方（模板引用反查的装配层）只需要拦住一次删除。
// 也刻意不带「该页是否待重建」：当前消费端读不出它（contenttemplate 的
// TemplateReference 没有对应字段），而契约字段是一份承诺 —— 先不给没人读的东西。
// 将来真要区分「已构建会坏 / 未构建不会坏」时再加，命中口径（活跃或暂存产物的依赖行）
// 本来就不依赖这个字段。
//
// 形状（不是 dto 重导出，也不是 model 的行类型）：它只有反查这一个消费者，
// 走 contract 定义可以避免为了两个字段在 dto 里再加一组结构 —— 先例是
// contenttemplate 契约里的 ResolvedTemplate。跨模块调用方只依赖 contract。
type DependencyPageRef struct {
	// ID 页面 id。
	ID string
	// Title 作者在文档 SEO 段里填的标题；**可能为空**（读侧不替它编一个名字）。
	Title string
}

// PageDependencyLookup 按依赖键**只读**反查页面引用（反查面）。
//
// 与 PageService 上已有的 MarkStaleByDependency 严格区分，两者签名相近、语义相反：
// 那条是**写路径** —— 它按 (kind,key) 把命中的页面 UPDATE 成 stale = true；
// 本接口只回答「谁声明过这条依赖」，实现里没有任何写语句（尤其不碰 stale 列）。
//
// 为什么必须单独成接口而不是复用 MarkStaleByDependency：拿一次只读反查去调写路径，
// 等于「只想看一眼影响面，却顺手改动了全站页面的 stale 列」—— 而且返回的 id 集合
// 与真实影响面还不同（写路径返回的是「这次被标记的」，反查要的是「现在声明着它的」）。
type PageDependencyLookup interface {
	// FindPagesByDependency 在**指定工程作用域内**按依赖键 (dependencyKind, dependencyKey)
	// 反查声明过该依赖的页面（最小投影，见 DependencyPageRef）。
	//
	// 命中口径：该页面的**活跃或暂存**产物在 page_dependencies 里声明了这条键 ——
	// 与 MarkStaleByDependency 的命中集合逐字一致（同一张表、同一对指针），
	// 于是「按依赖标记」与「按依赖反查」回答的永远是同一批页面。
	//
	// projectID 必填：pages 带 FORCE 策略，空工程 id 一律拒绝（**不**退化成「不限工程」，
	// 那会把别的工程的页面混进删除保护的影响面里）。可空入参不是错误：kind / key
	// 任一为空时返回空集合（不存在「按空键反查」这回事）。
	FindPagesByDependency(ctx context.Context, projectID, dependencyKind, dependencyKey string) (
		refs []DependencyPageRef, err error)
}

// PagePathKindLookup 按线上访问路径**批量**反查页面类型（只读）。
//
// 为什么是「按路径反查」而不是「列出全站路径」：消费方（文章页浏览量排行这类聚合）
// 手上只有一串**被访问过**的路径，它要的是「这些路径里哪些是文章页」。列出全站路径
// 会让调用方拿到成千上万条与本次统计无关的路径、再自己求一次交集，而每次统计只关心几十条。
//
// 刻意只回 kind，不回标题 / 文档 / 发布状态：带齐就成了第二个「页面列表」投影
// （ListPageTitles 已有），而这里的唯一用途是分类过滤。
type PagePathKindLookup interface {
	// KindsOfPaths 返回 path → kind，只含本工程内**未删除且已发布**（active_path 命中）的页面。
	//
	// 查不到的路径**不出现在结果里**：调用方据此排除它们，而不是猜一个默认类型 ——
	// 猜默认会让「已下线的文章页」继续被算进文章页统计，而且没有任何报错。
	// 路径为空、重复或全为空白时返回空 map（不是错误：调用方的路径集合本来就可能是空的）。
	KindsOfPaths(ctx context.Context, projectID string, paths []string) (map[string]string, error)
}

// BuildQueueEnqueuer 构建队列的入队端口（审计 DB-007）。
//
// 由 build 模块实现、装配期注入。接口定义在**本模块**（而不是反向 import build 的契约）：
// page 只需要「有人能把超限的重建任务接走」，不必知道队列是谁、存在哪里。
// 依赖方向因此是 build → page（build 用 page 的构建能力做执行器），不会成环。
//
// 未注入时保持既有行为：超出单次上限的页面保持 stale 并记一条告警（不静默丢弃）。
type BuildQueueEnqueuer interface {
	// projectID 是任务的工程作用域，由本模块显式带过来（审计 DB-01）：
	// 队列不反查来源表，而工程 id 在调用点本来就在手上（locatePageInProjects 已取到页面）。
	//
	// lang / intent 是任务上下文的另外两维（审计 ARCH-04），调用方必须一起给出：
	//   - lang：完整语言码（每种语言一条任务，队列的待办键含它 —— 见迁移 307）；
	//   - intent：BuildIntentManual（人工构建，只构建）或 BuildIntentDependency
	//     （依赖重建，构建 + 按旧发布范围回写线上）。
	// 参数顺序：两个字符串维度紧邻（lang, intent），随后是两个数值/哈希型输入版本。
	EnqueuePageBuild(ctx context.Context, pageID, projectID, lang, intent string, draftVersion int64, buildInputHash string) error
}

// 构建意图（与迁移 295 的 build_jobs_intent_check / buildmodel.Intent* 逐字对应）。
//
// page 侧自己留一份白名单常量，不在 page 里 import build 模块：依赖方向是
// build → page（队列的执行器由装配层接线），反向 import 会把这条依赖掰弯。
// 两处字面量由 public/test/page/unit 的对账用例钉住（不一致即红）。
const (
	// BuildIntentManual 人工发起：只构建（暂存），不回写线上。
	BuildIntentManual = "manual"
	// BuildIntentDependency 依赖失效自动重建：构建 + 按旧发布范围回写线上
	//（此前已发布的语言才重新发布，未发布过的语言留在暂存态）。
	BuildIntentDependency = "dependency"
)

// ==========================================================================
// 对外能力：别的模块能用 page 做什么
// ==========================================================================

// PageService 手工 Page 草稿、修订与发布管理能力。
type PageService interface {
	// SitePageResolver 槽位解析（构建期与片段层经这个只读面取「结算页在哪」）。
	SitePageResolver
	// PageDependencyLookup 按依赖键只读反查页面（反查面；写路径是下面的 MarkStaleByDependency）。
	PageDependencyLookup
	// PagePathKindLookup 按线上访问路径批量反查页面类型（只读，跨模块聚合用）。
	PagePathKindLookup

	Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error)
	// List 列出页面摘要（必须带 projectID；themeID 可选过滤主题）。
	List(ctx context.Context, req *pagedto.ListReq) (res []pagedto.PageResp, err error)
	// —— 页面级语言排除（迁移 491）——

	// —— 缺译报告（U2）——

	// UntranslatedPageLangs 列出该工程缺译的（页面 × 语言），只含 misses > 0，
	// 且已排除该语言的页面不出现在结果里（排除后不再产出，也就不存在缺译）。
	//
	// 数据源是产物 Manifest 的 translationMisses（构建期事实），不是实时重算 ——
	// 实时算会得出与线上字节不一致的第二份真相（译者补了译文但没重建）。
	UntranslatedPageLangs(ctx context.Context, projectID string) (rows []TranslationMissRow, err error)

	// PageLangStates 该页各启用语言的排除 / 发布状态（默认语言在前，后台面板展示用）。
	PageLangStates(ctx context.Context, pageID string) (rows []PageLangState, err error)
	// ExcludePageLang 排除某语言：下线该语言产物 + 清发布/暂存/路由/计划（同一事务）+ 写排除列。
	//
	// 返回下线掉的路径数（0 = 该语言本来没发布过，仍是成功的排除）。
	// 默认语言不可排除（ErrCannotExcludeDefaultLang）；已排除则 ErrLangAlreadyExcluded。
	ExcludePageLang(ctx context.Context, pageID, lang string) (retired int, err error)
	// RestorePageLang 解除排除（**不自动重新发布**：重新上线走常规发布入口，那一步有回执与回滚语义）。
	RestorePageLang(ctx context.Context, pageID, lang string) error

	// ListPageTitles 列出工程内页面的**标题投影**（id / 草稿路径 / 激活路径 / SEO 标题），
	// 必须带 projectID。
	//
	// 与 List 的分工：List 为列表页服务，刻意 omit 掉 draft_document 大字段，因此它的
	// PageResp.DraftDocument 恒为空、调用方**读不出标题**；只要标题和路径的消费方
	// （导航来源候选）用本方法 —— 标题在 SQL 侧取出，不把整份 JSONB 拉进 Go。
	// 需要完整文档时走 Detail（单页，只读一篇）。
	//
	// SEOTitle 为空表示作者没在文档 SEO 段填标题（读侧不替它编名字），调用方自行回退。
	ListPageTitles(ctx context.Context, projectID string) (res []pagedto.PageTitleResp, err error)
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
	// lang 为预览目标语言（空 = 站点默认语言，多语言 P2）；
	// canvasFrames 打开结构槽位的画布标记层（编辑器画布专用，见 core.RenderContext.CanvasSlotFrames）——
	// 它是给编辑器看的元信息，关掉时产物字节与「块内容直接写在页面里」逐字节一致。
	CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string, canvasFrames bool) (html []byte, err error)
	// Build 基于当前草稿构建并暂存产物（不激活线上）。
	Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error)
	// RunPageBuildJob 执行一条构建队列（source_type=page）任务，是队列执行器的执行体
	// （审计 ARCH-04：executor 不再裸调 Build(ID)）。
	//
	// 语义按任务意图分流，两条都走同一个单页重建编排（与同步路径同一份实现）：
	//   - manual：只构建任务携带的语言；
	//   - dependency：构建任务携带的语言，且该语言此前已发布时构建成功后重新发布。
	// 失败原样返回 —— 队列据此把任务标 failed（失败必须准确反映到任务上，不静默吞掉）。
	RunPageBuildJob(ctx context.Context, req *pagedto.PageBuildJobReq) (err error)
	// Publish 激活暂存产物。
	Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error)
	// PublishAllLanguages 一键发布全部启用语言（多语言开关开启时的发布口径）：
	// 按站点启用语言清单逐语言构建并激活，单语言失败不阻断其余语言。
	PublishAllLanguages(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishAllResp, err error)
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
	//
	// projectID 必填（DB-009 切角色收口）：page_site_slots 带 FORCE 策略，
	// 不带作用域时非超级角色读出来恒为空集（fail closed 不报错）。
	SiteSlotRefsOfPage(ctx context.Context, projectID, pageID string) (slots []string, err error)
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
	// SEOPatrol 站点 SEO 合规巡检（审计 SEO-01 前半段）：按激活清单逐份校验产物字节，
	// 输出 URL / 规则 / 证据 / ArtifactHash 齐备的报告。
	//
	// 确定性：结论只依赖本地产物字节 + 站点语言表 + 路由表，不联网、不查第三方；
	// 只读：不改产物、不改发布状态。它证明的是「产物内部自相矛盾吗」，
	// 不是线上收录与排名 —— 后者要由站长平台数据回答。
	SEOPatrol(ctx context.Context, req *pagedto.SEOPatrolReq) (res *pagedto.SEOPatrolResp, err error)
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
	// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用），
	// 返回**本次真正命中**的页面 ID（RETURNING id 的回读结果，不是入参回显）。
	//
	// 为什么要把 ids 透出来：调用方（块/主题变更传播器）拿到它才能**立刻重建**。
	// 「只标记、重建靠人工触发」的那条不对称正是「改了页眉块，后台显示待重建、
	// 线上一个月不变」的来源 —— 而人工入口当时并不存在。
	MarkStaleForTheme(ctx context.Context, themeID string) (ids []string, err error)
	// MarkStaleForBlock 把文档中经 core.globalref 引用或 settings.structure 页眉/页脚
	// 自选绑定该块的页面标记为待重建（块内容变更后调用，与 MarkStaleForTheme 互补）。
	// 返回值语义与 MarkStaleForTheme 逐字相同。
	MarkStaleForBlock(ctx context.Context, blockID string) (ids []string, err error)
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
	// CountBlockReference 统计引用该块的未删除页面数（globalref / structure 页眉·页脚·槽位绑定），
	// 供 block 模块删除或切换 global→template 前的引用拦截（docs/02-D §9）。
	CountBlockReference(ctx context.Context, blockID string) (int64, error)
	// ListBlockSourceRefs 列出引用该块的页面（审计 ARCH-02）：逐条给出页面路径与命中通道
	//（文档树 / settings.structure 槽位绑定），供装配层合并成块删除保护的判据。
	//
	// 与 CountBlockReference 的关系是「同一个事实的两种投影」：那条给计数（列表页影响面列），
	// 这条给可定位的实体与类别（删除拒绝时的提示）。
	ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)
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

	// ---- 定时上下线（PIPE-7）----
	//
	// 到点动作有两条路径（service/page_schedule_apply.go）：
	//   · 上线 = 把该语言的符号链接原子切到**排定时冻结的暂存产物**，再落数据库 ——
	//     **不重新编译**（编译是「按现在的站点环境产出一份字节」，与「把当时确认过的
	//     那一份推上去」是两件事）；
	//   · 下线 = 先删符号链接（或落一条 301 产物），再在一个事务里解除路由占用 +
	//     清该语言的发布指针。
	//
	// 排定后草稿被改 → 到点**硬失败**（置 failed 并在后台可见，key 取 ErrRebuildRequired），
	// 不按当前草稿重新编译。

	// SetPageSchedule 排定一次到点动作（publish / offline）。
	//
	// 上线排定内含一次 Build：排定即冻结产物（没有暂存产物就无从「只切指针」），
	// 「构建成功」同时是「这次排定可执行」的证明。
	// 时间入参按**站点时区**解释，落库一律 UTC（列是 timestamptz）。
	SetPageSchedule(ctx context.Context, req *pagedto.ScheduleSetReq) (res *pagedto.ScheduleItem, err error)
	// CancelPageSchedule 取消一条尚未执行的排定（running 与终态的行按不存在处理）。
	CancelPageSchedule(ctx context.Context, req *pagedto.ScheduleCancelReq) (err error)
	// ListPageSchedules 列出某页面的排定（新到旧，带上限）。
	ListPageSchedules(ctx context.Context, req *pagedto.ScheduleListReq) (res *pagedto.ScheduleListResp, err error)
	// ListSchedulesForPages 批量取这批页面的排定投影（后台列表页的行内徽标）：
	// 一次查询取回待执行与最近失败的记录，逐页问一次会把一次页面渲染变成几十次查询。
	ListSchedulesForPages(ctx context.Context, pageIDs []string) (res map[string]pagedto.SchedulePageSummary, err error)
	// RunDueSchedules 执行一轮到点扫描：回收超时租约 → 分批认领 → 逐条执行
	// （单条失败不中断整批）。调度器与运维手动触发共用这一个入口，
	// 与 PurgeRetention 的「后台手动触发与定时任务共用」同一形状。
	RunDueSchedules(ctx context.Context) (res *pagedto.ScheduleRunResp, err error)
}

// PageQueryReader 页面标题的只读视图：给 AI 工具的窄门。
//
// 为什么只要一个方法而不是整个 PageService：那个接口上有发布、回滚、删除页面、
// 改路径 —— 工具由模型驱动，给它写能力意味着「AI 顺手把一个页面下线了」在某次
// 无关改动里变得可能。越权防护靠接口形状。
//
// 为什么是「列出全部标题」而不是「按关键词查」：页面列表通道（service.List）
// 只按工程与主题筛，没有关键词参数，而它的返回体带着整份草稿文档（每个页面几百 KB）。
// 对「用户说个名字，找出那个页面」这件事，标题清单已经够用 —— 关键词匹配放在
// 工具层做（页面数量是百级，一次全拉进内存比对比给列表通道加一套关键词查询便宜得多，
// 也不必为它在 model 层开一条只为 AI 服务的索引）。
type PageQueryReader interface {
	// ListPageTitles 列出一个工程的全部页面标题（含草稿路径与线上路径）。
	ListPageTitles(ctx context.Context, projectID string) (res []pagedto.PageTitleResp, err error)
}
