package blockhttp

// 新建与删除。自 dashboard 迁回本模块；块内容编辑复用工作台（/workbench?block=ID）。
// stale 传播 / 引用检查已由装配层接管（internal/routers/block_page_bridge.go），
// 本页不再持有任何跨模块失效编排。

// 现象：块内容变更 / 删除后，装配层注入的 stale 传播器（internal/routers/block_page_bridge.go
// 的 BlockStalePropagator）会把「绑定了该块的主题下全部页面」与「文档里 globalref /
// settings.structure 引用了该块的页面」标记为 stale。但这件事对运营**完全不可见**：
// 块列表页只显示名称与更新时间，块编辑保存后除了页面上的徽章之外没有任何地方能回答
// 「改这个块会影响哪些页面」；reuse_mode=template 的块又**不传播**（插入时已复制 AST），
// 运营也无从区分这两类块。
//
// 本文件补的是**只读可见性**（本批不做自动重建）：
//   · 每个「引用」模式的块显示它当前被多少页面引用（page.CountBlockReference，只读）；
//     「复制」模式的块显示「—」并说明原因 —— 少一行数据不会误导人，
//     而把「不传播」的块也标上引用数会让人以为改它需要重建。
//   · 页面顶部汇总当前待重建页面数 + 清单（page.List 的 Stale 面）。
//
// 与 content 侧同名实现的取舍：两处各留一份约 40 行的只读统计，而不是抽公共包 ——
// 它依赖的是**各自的模块契约**（content 侧用 pagecontract + projectcontract，
// block 侧同一对），抽出去要么放进 page 模块（清单外）要么造成 block↔content 反向依赖。
// 两处的口径必须一致：改一边请同步另一边（页面上的文案也共用同一套 key 说明）。

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

// blocksPageTitle 全局块管理页标题（i18n key，与原 dashboard 枚举同值）。
const blocksPageTitle = "MsgBlocksTitle"

// blockPageHandle 全局块管理页处理器。
type blockPageHandle struct {
	blocks   blockcontract.BlockService
	projects projectcontract.ProjectService
	// pages 页面契约（只读用：块变更的影响面 —— 引用该块的页面数、待重建页面清单）。
	//
	// 可空：未注入时页面明确写「页面能力未装配，无法统计影响面」，而不是显示一个
	// 「0 个页面引用」的假结论（见 block_page.go）。注入点是装配层
	// mountAdminPages 里的 SetupBlockPages 调用（可选变参）。
	pages pagecontract.PageService
}

// NewBlockPageHandle 创建全局块管理页处理器。
//
// pages 为可选变参：装配层尚未传入时页面照常工作，只是不显示影响面。
func NewBlockPageHandle(blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) *blockPageHandle {
	h := &blockPageHandle{blocks: blocks, projects: projects}
	if len(pages) > 0 {
		h.pages = pages[0]
	}
	return h
}

// blockRow 全局块列表行投影。
type blockRow struct {
	ID        string
	Name      string
	Kind      string
	ReuseMode string
	UpdatedAt utils.JSONTime
	// -1 表示引用数查询失败，区别于零引用。
	RefCount int64
}

// blocksPageData 全局块管理页数据。
type blocksPageData struct {
	Title    string
	Menu     string
	Projects []projectcontract.ProjectResp
	// SelectedProject 模板键名（blocks.html 读 .SelectedProject）。
	SelectedProject string
	Headers         []blockRow
	Footers         []blockRow
	Blocks          []blockRow
	// StaleImpact 只读影响面：待重建页面数与清单（块 / 内容 / 主题 / 导航变更都会产生）。
	StaleImpact staleImpactView
}

// stalePageRow 待重建清单的一行（模板 blocks.html 的 {{.StaleImpact.Pages}}）。
type stalePageRow struct {
	ID          string
	Path        string
	ProjectID   string
	ProjectName string
}

// staleImpactView 只读影响面（模板 blocks.html 的 {{.StaleImpact.*}}）。
//
// 降级语义：pages 未注入 / 读取失败 → Available=false（显示「读不到」），
// 绝不渲染成「0 个待重建」—— 两者对运营的含义完全不同。
type staleImpactView struct {
	Available bool
	Pages     []stalePageRow
	Total     int
	Truncated bool
	Limit     int
	// Hint 取不到影响面时的说明（已取词）。
	Hint string
}

// toBlockRows 按 kind 精确过滤（header/footer 页眉页脚组）。
func toBlockRows(blocks []blockcontract.BlockResp, kind string) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind != kind {
			continue
		}
		rows = append(rows, toBlockRow(b))
	}
	return rows
}

// toOtherBlockRows 其余全部类型（新 kind + snippet 片段模板）归入「区块/复用资产」组。
func toOtherBlockRows(blocks []blockcontract.BlockResp) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind == "header" || b.Kind == "footer" {
			continue
		}
		rows = append(rows, toBlockRow(b))
	}
	return rows
}

func toBlockRow(b blockcontract.BlockResp) blockRow {
	return blockRow{
		ID: b.ID, Name: b.Name, Kind: b.Kind,
		ReuseMode: b.ReuseMode, UpdatedAt: b.UpdatedAt,
	}
}

// BlocksList 全局块管理页（GET /admin/blocks?project=X）。
func (h *blockPageHandle) BlocksList(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, adminui.MsgInternalError)
		return
	}
	data := &blocksPageData{
		Title:    blocksPageTitle,
		Menu:     "blocks",
		Projects: projects,
	}
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProject = sel
	} else if len(projects) > 0 {
		data.SelectedProject = projects[0].ID
	}
	if data.SelectedProject != "" {
		blocks, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: data.SelectedProject})
		if err != nil {
			response.ErrorWithMessage(c, http.StatusInternalServerError, adminui.MsgInternalError)
			return
		}
		data.Headers = toBlockRows(blocks, "header")
		data.Footers = toBlockRows(blocks, "footer")
		data.Blocks = toOtherBlockRows(blocks)
		// 只读影响面（见 block_page.go）：逐块引用页面数 + 待重建页面清单。
		h.fillRefCounts(c.Request.Context(), data.Headers)
		h.fillRefCounts(c.Request.Context(), data.Footers)
		h.fillRefCounts(c.Request.Context(), data.Blocks)
	}
	// 影响面在两种分支（有工程 / 无工程）下都要给：模板是同一份，
	// 缺这个键会让取值链中断（HTTP 仍 200、后半页整块消失）。
	data.StaleImpact = h.blockStaleImpact(c, c.Request.Context())
	// 直接交视图本身：blocksPageData 的字段名与模板键逐字一致，摊平只是多一次搬运，
	// 而搬运处拼错键名的表现是「页面 200 但少一块」（Jet 缺键中断）。
	pageview.Render(c, data)
}

// SaveBlockContent 工作台保存块内容（POST /admin/blocks/save-content，JSON）。
// 工作台块编辑的保存入口：保存后由 block service 统一编排 stale
// （传播器已在装配层注入，REST /api/block/update 与本保存路径同源传播）。
func (h *blockPageHandle) SaveBlockContent(c *gin.Context) {
	// 请求体形状（含 ReturnURL 的来源与语义）在 blockdto.SaveContentReq。
	req, err := binding.ParseJSON[blockdto.SaveContentReq](c)
	if err != nil {
		// JSON 端点：出口必须是 JSON。调用方是工作台的 saveDraft，它对响应做 r.json()、
		// 再按 code >= 400 取 message 弹提示；原先的 c.String(400, "参数不合法") 是纯文本，
		// r.json() 直接 reject 落入 catch —— 用户只看到保存状态变红，一句话提示都没有。
		// 归口走本包既有的 paramBindFail（400 + 受控文案 + 绑定原文只进 warn 日志）。
		paramBindFail(c, err)
		return
	}
	// 名称取现值（工作台只改文档）。
	current, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: req.ID})
	if err != nil {
		// 状态码与文案都归口到 REST 出口用的同一份判定（block_handle.go）：
		// 业务错误（块不存在 / 缺工程作用域）→ 各自的状态码 + 模块词条 key（由 response
		// 按请求语言翻译）；基础设施故障 → 500 + 归口文案，原文只进日志。
		//
		// 原先这里是 `if err != nil || current == nil` 一把吞成 404 纯文本：既把
		// 「读不到库」伪装成「块不存在」，又让整条 JSON 契约断在这里。
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	if current == nil {
		// 契约上「Detail 无错」即命中，这是防御分支：静默继续会把一次写操作建在空块上。
		response.ErrorWithMessage(c, http.StatusNotFound, blockenums.ErrBlockNotFound)
		return
	}
	name := current.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	if _, uerr := h.blocks.Update(c.Request.Context(), &blockcontract.UpdateReq{
		ID: req.ID, Name: name, Document: req.Document, ExpectedVersion: req.ExpectedVersion,
	}); uerr != nil {
		// 一次 ErrorAuto 覆盖两档：版本冲突是业务错误（service 声明了 409 + `key: 明细`），
		// 其余走内部错误归口（原文只进日志）。控制器不再自己分档 ——
		// 状态码与文案都在错误声明处（AGENTS.md「响应与文案」）。
		response.ErrorAuto(c, http.StatusConflict, blockErrScene, uerr)
		return
	}
	// 回跳（303 PRG）：只有**站内相对路径**才接受，其它一律拒绝并落默认列表页。
	//
	// 开放重定向在后台同样是「看起来像本站自己发起的跳转」：//evil.example.com 是协议
	// 相对 URL，浏览器会当外站处理；绝对 URL 更直接。判据与语言切换回跳共用同一份
	//（pkg/safepath.LocalReturnPath，task-75 自 shell 迁入）。没传（普通块编辑）时不跳，
	// 保存后留在工作台。
	if raw := strings.TrimSpace(req.ReturnURL); raw != "" {
		target := safepath.LocalReturnPath(raw)
		if target == "" {
			target = "/admin/blocks"
		}
		c.Redirect(http.StatusSeeOther, target)
		return
	}
	// 成功回执走 response 出口（与上面错误路径同族）：message 传 key，由 response 层按键取词，
	// 不再把中文写在这里 —— 原先在控制器里手拼一个只含 message 的 map 直接返回
	// 是英文界面上唯一说中文的那一句（同文件其余路径都走了 response + enums）。
	response.SuccessWithMessage(c, blockenums.ContentSavedRebuildQueued)
}

// blockErrScene 页面出口的日志场景名（与 block 模块其它 logger.Scene("block") 调用点一致）。
const blockErrScene = "block"

// blockErrFact 一次错误的**展示事实**：判定 + 归口 + 日志都在 adminui（判据只有一条）。
//
// 用 Quiet 变体（**不记日志**）：本模块的真调用点（批量删除的逐条原因、单条删除的 409 拒绝）
// 都在调用前记过一条更具体的日志（带 block_id / op / path），这里再记一次会让同一个错误
// 在日志里出现两遍 —— 改造前 blockErrText 也不记日志，这是一条行为不变项。
func blockErrFact(c *gin.Context, err error) adminui.ErrFact {
	return adminui.PageErrorFactQuiet(c, blockErrScene, err)
}

// blockErrText 业务错误 → 当前语言文案。
//
// 判定收敛到 adminui.PageErrorFact（pkg/response.IsBusinessError 的**形态**判据）：
// 业务错误的取值本来就是 enums 常量、而 enums 的值即 i18n key（真文案在 sys_i18n，
// 迁移 058 + 237 已 seed 全部 ErrBlock* 词条），所以「认得出形态 = 可以直接给用户看」。
//
// 原先这里是 blockErrSentinels + errors.Is 的**第二份白名单** —— 它是第二份真相：
// service 换了措辞、或某个 key 改由别的模块提供就静默失配，而两种症状都不报错
// （有时把内部错误当业务文案透出、有时把业务文案吞成归口）。判定只有一份之后，
// 「业务错误必须能被认出来」这条守卫改由共享判据承担（见 block_page_handle_test.go）。
//
// 兜底仍是 key 本身（与改造前逐字一致）：词条缺失时页面显示裸 key，一眼可见且可查；
// 空串会让失败在页面上**完全不可见**，用户以为操作成功了。
func blockErrText(c *gin.Context, err error) string {
	if err == nil {
		// 拿不到任何错误时也给归口文案（页面/逐条结果不能出现空白）——
		// 与改造前的形状一致；真调用点都带非 nil err。
		return adminui.MsgText(c, adminui.InternalMsg())
	}
	return adminui.ErrorFactText(c, blockErrFact(c, err))
}

// blockStalePageLimit 影响面清单一次列出的页面数（与 /admin/pages 的 staleOverviewLimit 同值）。
//
// 消费者口径，定义在调用方：不填会落到 model 的 50 条默认，而只读区块的作用是让人**看见**
// 影响面、不是给出完整清单（被截断的条数由 Total 给出并在页面上说明）。
const blockStalePageLimit = 8

// 影响面「读不到」的两条提示（事实：key + 中文兜底）。
//
// 包级变量并**不带 gin.Context**：这段文案属于哪个语言，由取词发生的时刻决定，
// 与控制器无关 —— 控制器不取词（见批次约定 .cache/wp-refactor-A-brief.md）。
// 两条分开是**故意的**：一条是「本页永远查不了（能力没装）」、另一条是「这一次没读出来」，
// 处置方向不同（查部署 vs 刷新重试）。
var (
	blockImpactNoPageCapabilityMsg = adminui.Msg{
		Key:      blockenums.ImpactUnavailableNoPageCapability,
		Fallback: "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。",
	}
	blockImpactProjectReadFailedMsg = adminui.Msg{
		Key:      blockenums.ImpactUnavailableProjectReadFailed,
		Fallback: "读取站点工程失败，本次无法统计待重建影响面。",
	}
)

// blockStaleImpact 取「全站待重建」影响面的数据（只读观测）。
//
// 取数走 page 契约的 ListStalePages —— 与 /admin/pages 的「全站待重建」、/admin/articles 的
// 「待重建影响面」是同一个查询。**这里曾经是逐工程 List + 自行截断的第二份实现**：
// 那份注释说「抽公共包要么放进 page 模块」，而 ListStalePages 就是那个落点，
// 于是三处口径合并成一份（此前三处的数与清单长度都不保证相同）。
//
// 降级语义：pages 未注入 / 读取失败 → Available=false（显示「读不到」），
// 绝不渲染成「0 个待重建」—— 两者对运营的含义完全不同。契约返回 (nil, nil) 是异常形态，
// 同样按读不到处理。
func (h *blockPageHandle) blockStaleImpact(c *gin.Context, ctx context.Context) staleImpactView {
	// Hint 由**事实**取成当前语言文本（取词走 adminui.MsgText，不再经 shell）：
	// 这个槽位（模板 blocks.html 的 {{.StaleImpact.Hint}}）还没取词化 —— 优先形态是
	// 把 key 交给模板、由它用注入的 tr 取值；本轮先按批次约定取成文本再传（见批次报告）。
	unavailable := func(msg adminui.Msg) staleImpactView {
		return staleImpactView{
			Available: false, Pages: []stalePageRow{}, Total: 0, Truncated: false,
			Limit: blockStalePageLimit, Hint: adminui.MsgText(c, msg),
		}
	}
	if h == nil || h.pages == nil {
		return unavailable(blockImpactNoPageCapabilityMsg)
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		Limit:      blockStalePageLimit,
		Descending: true,
	})
	if err != nil {
		// 工程表为空时契约返回 ErrProjectRequired：与真正的读取失败合并显示为「读不到」，
		// 因为跨模块 import page/service 取那个哨兵是禁止的（AGENTS.md 模块边界）。
		logger.Scene("block").Error(err, "读取全站待重建清单失败，待重建影响面本次不可用")
		return unavailable(blockImpactProjectReadFailedMsg)
	}
	if res == nil {
		logger.Scene("block").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable(adminui.Msg{})
	}
	pages := make([]stalePageRow, 0, len(res.Pages))
	for i := range res.Pages {
		pages = append(pages, stalePageRow{
			ID:          res.Pages[i].ID,
			Path:        res.Pages[i].Path,
			ProjectID:   res.Pages[i].ProjectID,
			ProjectName: res.Pages[i].ProjectName,
		})
	}
	return staleImpactView{
		Available: true, Pages: pages, Total: res.Total, Truncated: res.Truncated,
		Limit: blockStalePageLimit, Hint: "",
	}
}

// fillRefCounts 查询引用数，-1 表示无法读取，交由模板区分于零引用。
func (h *blockPageHandle) fillRefCounts(ctx context.Context, rows []blockRow) {
	for i := range rows {
		rows[i].RefCount = h.blockRefCount(ctx, rows[i])
	}
}

// blockRefCount 该块当前被多少页面引用（只读；page.CountBlockReference 同一口径，
// 也就是删除拦截用的那个计数）。pages 未注入或查询失败返回 -1，页面显示为未知。
//
// 只对 reuse_mode=global 的块算：template 块插入时已复制 AST，改它不影响任何页面，
// 给它标一个引用数会让人以为「改它需要重建」。
func (h *blockPageHandle) blockRefCount(ctx context.Context, b blockRow) int64 {
	if h == nil || h.pages == nil {
		return -1
	}
	if b.ReuseMode != blockmodel.ReuseGlobal {
		return -1
	}
	n, err := h.pages.CountBlockReference(ctx, b.ID)
	if err != nil {
		logger.Scene("block").With("block_id", b.ID).Error(err, "统计块引用页面数失败（影响面显示为未知）")
		return -1
	}
	return n
}

// BlocksStaleDrawer 待重建页面清单的**只读抽屉**片段（GET /admin/blocks/stale/drawer）。
//
// 与 /admin/articles 的同名抽屉共用片段模板与词条：两处说的是同一件事（同一份 ListStalePages），
// 各写一份模板的下场是同一个现象在两个页面上有两种样子。
//
// 取不到数据一律只给状态码：缺 data-drawer-fragment / 缺列的片段会被 drawer.js 的
// fragmentRoot 校验判非法，用户看到的同样是「加载失败」，但那时还多花了一次渲染。
func (h *blockPageHandle) BlocksStaleDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.pages == nil {
		c.Status(http.StatusNotFound)
		return
	}
	res, err := h.pages.ListStalePages(c.Request.Context(), &pagecontract.StalePageListReq{
		Limit:      blockStalePageLimit,
		Descending: true,
	})
	if err != nil {
		logger.Scene("block").Error(err, "读取全站待重建清单失败（抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	if res == nil {
		logger.Scene("block").Warn("全站待重建清单返回空结果（契约实现异常，抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	rows := make([]staleDrawerRow, 0, len(res.Pages))
	projects := make(map[string]struct{}, 2)
	for i := range res.Pages {
		projects[res.Pages[i].ProjectID] = struct{}{}
		rows = append(rows, staleDrawerRow{
			ID:          res.Pages[i].ID,
			Path:        res.Pages[i].Path,
			ProjectName: res.Pages[i].ProjectName,
			Published:   res.Pages[i].Published,
			// 失败痕迹（迁移 474）：有痕迹说明这页不是「还没轮到」，而是重建失败过。
			// 交的是事实（阶段 key + 时刻），整句由共用的片段模板拼回。
			RebuildFailure: rebuildFailureViewOf(res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}
	// 片段出口与整页共用 pageview.Render —— 套不套页壳由路由的 BareSlot / PageSlot 决定，
	// 控制器不持模板名、不 import 渲染层（与整页同一形态）。
	pageview.Render(c, stalePagesDrawerView{
		Rows: rows, Total: res.Total, Truncated: res.Truncated,
		Limit: blockStalePageLimit, MultiProject: len(projects) > 1,
	})
}

// staleDrawerRow 待重建抽屉片段的一行（模板 admin/partials/stale_pages_drawer.html）。
type staleDrawerRow struct {
	ID             string
	Path           string
	ProjectName    string
	Published      bool
	RebuildFailure rebuildFailureView
}

// stalePagesDrawerView 待重建抽屉片段的渲染视图（**片段出口**，不是整页）。
type stalePagesDrawerView struct {
	Rows         []staleDrawerRow
	Total        int
	Truncated    bool
	Limit        int
	MultiProject bool
}

// rebuildFailureView 失败痕迹的**事实**：阶段词条 key + 中文兜底 + 已格式化的时刻。
//
// 取词交给渲染期（片段模板用注入的 tr 拼回整句）：语言是请求级的，在控制器里提前翻等于
// 替渲染期做决定 —— 英文后台下会显示中文，且不报错、没日志。
// 本类型与 content 模块的同名类型是两份（跨模块共用要走契约，而这是纯展示装配）。
// 阶段 → 文案用 blockenums 的 ImpactStage* 常量，与 page/enums 的 RebuildStageLabel 同持
// 一对 key（content 侧已改为直接调那个函数）：改一处请同步另一处。
type rebuildFailureView struct {
	// StageKey / StageFallback 失败阶段文案；未登记的阶段 Key 空、Fallback 是原始阶段值。
	StageKey      string
	StageFallback string
	// At 失败时刻（已格式化的墙上时间；时间格式与语言无关）。
	At string
	// Has 是否有失败痕迹（两列都空 = 没有）。
	Has bool
}

// rebuildFailureViewOf 迁移 474 的两列 → 失败痕迹事实（**不含错误原文**）。
func rebuildFailureViewOf(stage string, at *utils.JSONTime) rebuildFailureView {
	if strings.TrimSpace(stage) == "" && at == nil {
		return rebuildFailureView{}
	}
	view := rebuildFailureView{Has: true, StageFallback: stage}
	switch stage {
	case "plan":
		view.StageKey, view.StageFallback = blockenums.ImpactStagePlan, "计划阶段（站点语言清单或旧发布范围读不到）"
	case "build":
		view.StageKey, view.StageFallback = blockenums.ImpactStageBuild, "构建 / 发布阶段"
	}
	if at != nil {
		view.At = time.Time(*at).Local().Format("2006-01-02 15:04")
	}
	return view
}
