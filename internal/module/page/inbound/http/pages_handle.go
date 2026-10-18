package pagehttp

// pages_handle.go — 页面本体：后台页面管理页（页面面）与页面本体的 JSON 接口面，
// 外加版本历史（页面本体的一段）。
//
// 为什么三者在同一个文件（internal/module/CLAUDE.md §inbound/http「一个文件 = 一个业务功能」）：
// 它们说的是同一个东西（一个 Page），改「页面能不能删 / 路径怎么改 / 修订怎么回滚」时
// 三处必须一起改 —— 按「页面面 / 接口面 / 历史」拆成三个文件，代价是同一件事的判据
// 散在三处，而拆分的收益（少翻几屏）由文件内的 `// ===` 分段就能拿到。
//
// 分段（页面面在前、接口面在后、历史殿后）：
//   · `// === 页面面（后台页面管理页）===`：列表页、列表内的写动作、列表页自己的回执；
//   · `// === 接口面（JSON API）===`：/api/page/* 的处理器、错误 → 状态码 / 消息的映射、
//     Handle / NewHandle（模块入口，装配层从这里取句柄）；
//   · `// === 版本历史（页面本体的一段）===`：修订列表片段与「用某个修订覆盖草稿」。
//
// 交互遵循后台 HTMX 规范：HTMX 请求返回 Jet 片段，否则完整页面/重定向。

import (
	"context"
	"errors"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/shell"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

// === 页面面（后台页面管理页）===
// 页面管理列表页（后台「页面」入口）：列出/新建站点工程与页面，
// 行内直达可视化工作台。交互遵循后台 HTMX 规范：HTMX 请求返回
// Jet 片段，否则完整页面/重定向。

// 批量操作的结论（AGENTS.md：「结论按『成功 N / 跳过 M』回带，不允许静默的部分成功」）
// 现在由 shell.RenderJump 渲染成整页提示：句子在响应体里，不进 URL，所以不存在
// 「URL 里的计数可被客户端改写」这条伪造面（旧形态 `?done=已删除 3 个页面` 曾靠读侧
// 归一化比对来防伪）。有跳过走失败档（不自动跳，用户下次会去看剩下那些）。
// pagesAdminHandle 页面列表与翻译工作台的页面处理器（从 dashboard 回迁）。
// 只持有本页面需要的契约；构造见 NewPagesAdminHandle。
type pagesAdminHandle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService
	// blocks 全局块契约（只读）：翻译工作台收集页眉/页脚绑定与 globalref 内的候选。
	blocks blockcontract.BlockService
	// blueprints 蓝图候选（新建页面时的空白草稿模板）。
	// 可空：端口未注入时表单不显示蓝图选项，建页照常（blueprintOptions 返回空切片）。
	blueprints blueprintcontract.BlueprintService
	// siteIndex 全站可翻译内容索引缓存（跨页面复用提示 + 全站完成度，见
	// 同模块的 page_translations_handle.go）。
	siteIndex siteContentIndexCache
	// contentStore 内容译文读写端口（翻译工作台）。
	// 为 nil 时按默认实现（pkg/i18n.ContentWriter + 默认数据库）惰性构造；
	// 测试经 SetContentTranslationStore 注入隔离 schema 的写入器。
	contentStore contentTranslationPort
}

// NewPagesAdminHandle 创建页面列表 / 翻译工作台处理器；各契约为对应模块 contract。
func NewPagesAdminHandle(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, blueprints blueprintcontract.BlueprintService) *pagesAdminHandle {
	return &pagesAdminHandle{pages: pages, projects: projects, blocks: blocks, blueprints: blueprints}
}

// PagesAdminHandle 导出类型别名：外部测试包（public/test/page/feature）需要命名
// 构造器返回的句柄类型；别名指向未导出类型是合法 Go，读起来也明确指向后者。
type PagesAdminHandle = pagesAdminHandle

// SetBlueprints 注入蓝图契约（装配期调用；可空）。
func (h *pagesAdminHandle) SetBlueprints(b blueprintcontract.BlueprintService) { h.blueprints = b }

// blueprintOptions 拉取蓝图候选；蓝图端口未注入或查询失败时返回空列表
// （表单不显示蓝图选项，建页照常走空白草稿）。
func (h *pagesAdminHandle) blueprintOptions(ctx context.Context) []blueprintOption {
	if h.blueprints == nil {
		return nil
	}
	list, err := h.blueprints.List(ctx, &blueprintdto.ListReq{})
	if err != nil {
		logger.Scene("page").With("err", err).Warn("蓝图列表读取失败，新建页面表单不显示蓝图选项")
		return nil
	}
	out := make([]blueprintOption, 0, len(list))
	for _, b := range list {
		out = append(out, blueprintOption{ID: b.ID, Name: b.Name})
	}
	return out
}

// pagesPageData 页面列表页数据。
// 模板键统一小写（admin/layout.html 以 {{.title}}/{{.menu}} 取值，
// Jet 对 map 键不做大小写兜底）。
type pagesPageData struct {
	Title    string
	Menu     string
	Projects []projectcontract.ProjectResp
	Pages    []pagedto.PageRowResp
	// Blueprints 蓝图候选（审计 VIS-010）：「从蓝图开始」是新建页面流程里的一个选项，
	// 不是另一个需要先去的页面。
	Blueprints []blueprintOption
	// Err 是**列表取数失败**的提示条（写动作的结论走 shell.RenderJump，不再回带）。
	// 批量结果按「已删除 N 个页面 / 跳过 M 个」渲染进提示页，不在这里回显。
	//
	// 交的是**事实**（key + 中文兜底 + `%s` 参数）而不是拼好的句子：取词在模板里做。
	// 交成品串会把这一条提示锁死在一种语言，而它恰恰是故障时唯一能看的东西。
	Err pageMsg

	// 发布回执收敛的只读观测（本轮接入）：待收敛条数 / 最老一条已等待多久 / 本进程最近一次
	// 收敛时刻。列表页是运维每天的落点，积压只写在日志与 /readyz 里等于不可见 ——
	// /readyz 又不参与就绪判定，没有人会因为它去看。
	//
	// ReceiptAlert 由条数派生（> 0），模板据此在「警告条」与「正常」之间分流：
	// 把判断留在 Go 侧，模板不必为 int64 与字面量的类型匹配操心（Jet 的比较要求同型）。
	ReceiptPending int64
	ReceiptOldest  string
	// ReceiptLastConverge 最近一次收敛时刻（事实）：格式由模板的 dateTimeSecond() 决定，
	// nil = 本次没读到（模板据此不渲染收敛后缀）。
	ReceiptLastConverge *utils.JSONTime
	ReceiptAlert        bool
	// ReceiptKnown 这份回执观测**这次请求真的读到了**（false = 本页装载失败、走降级渲染）。
	//
	// 为什么需要它：降级渲染时 ReceiptPending 等是零值，而模板会把「零积压」
	// 渲染成「发布回执收敛正常」—— 那是**错误的乐观断言**（这一页根本没读到回执状态）。
	// 判据在 handler 算好，模板只读一个布尔（与 project 域主题页的 NoProjectEmpty 同形：
	// 模板是磁盘热读文件，Go 侧改动要等重编译，判据写在模板里会随两边不同步而漂移）。
	ReceiptKnown bool

	// SelectedProject 当前聚焦的站点工程 id（筛选栏下拉的回显值；无工程时为空串）。
	SelectedProject string
	// FilteredProject 用户是否**指定**了工程（?project= 命中工程列表）。
	//
	// 空态据此分档：指定了工程却没页面 → 「这个站点工程还没有页面」（下一步是换工程或就地建页），
	// 否则是「还没有页面」（下一步是建第一个页面）。判据留在 Go 侧：模板是磁盘热读文件，
	// 判据写在模板里会随两边不同步而漂移（与 ReceiptKnown 同一理由）。
	FilteredProject bool

	// StaleOverview 全站待重建区块的渲染数据（只读观测，取数见 staleOverview）。
	//
	// 三种状态由**键 + Available** 一起表达，模板只读它们、不做取数：
	//   · nil（键不存在）→ 本次请求没装配这份数据（整页装载失败走降级渲染）：整块不渲染，
	//     顶部已有归口提示（Err），不在这里重复第二遍；
	//   · Available=false → 读不到（ListStalePages 失败）：显示「本次读不到」，
	//     绝不显示成「0 个待重建」（那会把一次读取失败渲染成「一切正常」）；
	//   · Available=true → 由 Total 分流「折叠清单」与「当前没有待重建的页面」。
	//
	// 为什么与 ReceiptKnown 的形态不同（那里总是给键、用布尔分流）：那份观测只有「读到 / 没读到」
	// 两种状态；这份还有「读到了但是空」这一种，而 nil 切片与空切片在模板里长得一样 ——
	// 必须由比较列表多一个 Available 才能分开。两者共用的判据是**降级渲染时不得给出乐观结论**。
	StaleOverview gin.H
}

// blueprintOption 新建页面表单里的蓝图选项。
type blueprintOption struct {
	ID   string
	Name string
}

// templateMap 转为模板所需的小写键 map。
func (d *pagesPageData) templateMap() gin.H {
	return gin.H{
		"title":      d.Title,
		"menu":       d.Menu,
		"Projects":   d.Projects,
		"Pages":      d.Pages,
		"Blueprints": d.Blueprints,
		"Err":        d.Err,

		"ReceiptPending":      d.ReceiptPending,
		"ReceiptOldest":       d.ReceiptOldest,
		"ReceiptLastConverge": d.ReceiptLastConverge,
		"ReceiptAlert":        d.ReceiptAlert,
		"ReceiptKnown":        d.ReceiptKnown,

		"SelectedProject": d.SelectedProject,
		"FilteredProject": d.FilteredProject,

		// 全站待重建区块（可选键）：字段为 nil 时这里输出 nil，模板的 isset 判为假
		//（Jet 的 isset 同时覆盖「键不存在」与「值为 nil」两种情形）→ 整块不渲染。
		"StaleOverview": d.StaleOverview,
	}
}

// PagesList 页面列表页。
//
// 装载失败**降级渲染**（空列表 + 归口提示，HTTP 200）：页面结构必须保留 ——
// 换菜单、去别的页面、刷新重试都还得能用。原先这里是 500 + `response.ErrorWithMessage`
// （一块 JSON），浏览器停在 JSON 上，用户既看不到列表也无从判断「是这一页没读出来、
// 还是整个后台坏了」。原文只进日志（pageErrPageText）。
func (h *pagesAdminHandle) PagesList(c *gin.Context) {
	data, err := h.buildPagesData(c)
	if err != nil {
		data = &pagesPageData{
			Title: pageenums.MsgPagesTitle, Menu: "pages",
			// 装载失败是**这次请求**真实发生的事，提示条据此渲染。
			Err: pageErrMsg(c, err),
			// ReceiptKnown 留 false：这次没读到回执状态，模板不该报「收敛正常」。
		}
	}
	c.HTML(http.StatusOK, "admin/page/pages", adminui.Prepare(c, data.templateMap()))
}

// buildPagesData 组装列表页数据。
//
// 页面按主题浏览（020_themes.sql：主题下面才是页面）：取当前聚焦工程的
// 激活主题过滤页面；无工程或无主题时 themeID 为空列全部页面。
//
// 「当前聚焦工程」由筛选栏的 ?project=<id> 决定（审计 02-L §2 P1-12）：这一页此前只能看
// 第一个工程的页面，工程一多就无从切换。维度取自 service —— page Service.ListReq.ProjectID
// 本来就支持它，缺的只是「有人从 query 读它」。未指定 / 指定的工程不在列表里（陈旧链接、
// 手改的 URL、刚被删）时回退到第一个工程：一条过期的 URL 不该把整页变成错误页。
func (h *pagesAdminHandle) buildPagesData(c *gin.Context) (*pagesPageData, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	// 第一步：定位当前聚焦工程（筛选栏的输入）。
	projectID, filteredProject := focusProjectID(projects, c.Query("project"))
	// 第二步：取该工程的激活主题。
	themeID := ""
	if projectID != "" {
		if theme, err := h.projects.GetActiveTheme(ctx, projectID); err == nil && theme != nil {
			themeID = theme.ID
		}
	}
	// 第三步：按聚焦工程与激活主题取页面。
	pages, err := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: projectID, ThemeID: themeID})
	if err != nil {
		return nil, err
	}
	// 排定投影（PIPE-7）：一次批量取回这批页面的待执行 / 最近失败记录。
	// 读取失败只记日志、页面照常渲染：与回执观测同一口径 ——
	// 一个附加观测不该让整张列表页 500（缺的是两个徽标，不是列表本身）。
	schedules := map[string]pagecontract.SchedulePageSummaryResp{}
	pageIDs := make([]string, 0, len(pages))
	for i := range pages {
		pageIDs = append(pageIDs, pages[i].ID)
	}
	if len(pageIDs) > 0 {
		got, serr := h.pages.ListSchedulesForPages(ctx, pageIDs)
		if serr != nil {
			logger.Scene("page").Error(serr, "读取页面排定投影失败（列表页不显示定时徽标）")
		} else {
			schedules = got
		}
	}
	rows := make([]pagedto.PageRowResp, 0, len(pages))
	for _, p := range pages {
		row := pagedto.PageRowResp{
			ID: p.ID, ProjectID: p.ProjectID, Kind: p.Kind,
			DraftPath: p.DraftPath, Active: p.ActiveArtifactID != nil,
			Staged: p.StagedArtifactID != nil, Stale: p.Stale,
			Version: p.DraftVersion, UpdatedAt: p.UpdatedAt,
		}
		if summary, ok := schedules[p.ID]; ok {
			row.SchedulePendingAt = schedulePendingAt(summary.Pending)
			if summary.Failed != nil {
				row.ScheduleAlert = true
				failure := scheduleFailureFact(summary.Failed.LastError)
				row.ScheduleFailedKey, row.ScheduleFailedFallback = failure.Key, failure.Fallback
			}
		}
		rows = append(rows, row)
	}
	// 待收敛回执观测（只读）：读取失败只记日志，页面照常渲染 ——
	// 一个观测字段不该让整张列表页 500。
	receiptPending, receiptOldest, receiptLast := h.receiptBacklog(ctx)
	// 全站待重建区块（只读）：取数作用域是**全部站点工程**，与上面按工程聚焦的页面列表
	// 不是一个数（块 / 文章 / 主题 / 词条改动影响的是全站）。读不到时它自己给失败态，
	// 同样不让整张列表页失败。
	stale := h.staleOverview(ctx)
	return &pagesPageData{
		Title: pageenums.MsgPagesTitle, Menu: "pages",
		Projects: projects, Pages: rows,
		// 筛选栏的两个键：SelectedProject 供下拉回显，FilteredProject 供空态分档
		//（「这个工程还没有页面」≠「全站还没有页面」，两者的下一步动作不同）。
		SelectedProject: projectID,
		FilteredProject: filteredProject,
		// 蓝图候选（审计 VIS-010）：把「从蓝图开始」放进新建页面流程，
		// 而不是要求编辑者先去另一个页面建好蓝图再回来。
		Blueprints: h.blueprintOptions(ctx),

		ReceiptPending:      receiptPending,
		ReceiptOldest:       receiptOldest,
		ReceiptLastConverge: receiptLast,
		ReceiptAlert:        receiptPending > 0,
		// 走到这里说明本页数据装配完成（回执观测读不到只记日志、不给零值以外的信号，
		// 见 receiptBacklog），所以这条观测条可以展示。
		ReceiptKnown: true,

		// 全站待重建区块：非 nil 即「本次装配到了这份数据」，读到与否由 Available 表达。
		StaleOverview: stale,
	}, nil
}

// focusProjectID 从工程列表里挑出当前聚焦的工程 id。
//
// want 来自筛选栏的 ?project=（`<select name="project">` 提交的 get 参数）。
// 命中即采纳；未命中（空串 / 伪造 id / 刚被删的工程）回退到第一个工程 —— 那是本页既有的
// 默认语义，回退而不是报错是因为：一条过期的 URL 不该把整页变成错误页，用户要的是列表。
//
// 第二个返回值表示「用户的指定真的被采纳了」：模板据此把空态分成两档
// （指定了工程却没页面，与全站还没有页面，下一步动作不同）。判据放这里而不是模板里，
// 是因为模板里的「有没有筛过」只能靠 query 猜，而这里同时知道 query 与工程列表。
func focusProjectID(projects []projectcontract.ProjectResp, want string) (id string, filtered bool) {
	want = strings.TrimSpace(want)
	if want != "" {
		for i := range projects {
			if projects[i].ID == want {
				return projects[i].ID, true
			}
		}
	}
	if len(projects) == 0 {
		return "", false
	}
	return projects[0].ID, false
}

// CreateProject 新建站点工程。
//
// 出口：结论由 shell.RenderJump 渲染成整页提示（见 page_jump.go）—— 失败不自动跳
// （运营要看清楚原因），成功 1 秒后自动回列表页。表单是原生
// `<form method="post" action="/admin/projects/create">`（admin/pages.html 的
// #tpl-project-create 抽屉，不是 hx-post）。
//
// 为什么不是 `c.String(400, …)`：那会把用户导航到一块只有一行字的页面上，
// 抽屉、页壳、他刚填的名称一并丢失。
func (h *pagesAdminHandle) CreateProject(c *gin.Context) {
	req, ok := binding.Form[pagedto.ProjectCreateFormReq](c)
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		pageListJump(c, false, pagesLocalNoticeProjectNameRequired)
		return
	}
	if _, err := h.projects.Create(c.Request.Context(), &projectcontract.CreateReq{
		Name: name, Settings: json.RawMessage("{}"),
	}); err != nil {
		logger.Scene("page").With("name", name).Error(err, "创建站点工程失败")
		// 页面路径的文案出口：业务 sentinel 翻成中文，其余落归口文案
		//（原文只进日志 —— 上面那条日志已带 name，这里用不记日志的变体，免得同一错误记两遍）。
		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	pageListJump(c, true, pageProjectCreatedMsg())
}

// CreatePage 新建页面（默认空白草稿，创建后可进工作台编辑）。
//
// 出口同 CreateProject：原生表单 + 整页提示（取代原先的 303 + ?err=）。
func (h *pagesAdminHandle) CreatePage(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageCreateFormReq](c)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	path := strings.TrimSpace(req.DraftPath)
	// 两条必填分开报，不合成一句「项目与页面路径不能为空」：合成句把「没选工程」
	// 与「没填路径」说成同一件事，而两者的修法完全不同（选择器 vs 输入框）。
	if projectID == "" {
		pageListJump(c, false, pageMsg{Key: pageenums.ErrProjectRequired, Fallback: pageenums.ErrProjectRequired})
		return
	}
	if path == "" {
		pageListJump(c, false, pagesLocalNoticePathRequired)
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 默认空白草稿：layout.mode 为编译端必填校验项（full/boxed）。
	// 选了蓝图则以蓝图为准（审计 VIS-010）：page.Create 会用 InitPageDocument 复制
	// 蓝图 AST 并重生成节点 ID，这里传的空白文档只是「没选蓝图」时的兜底。
	if _, err := h.pages.Create(c.Request.Context(), &pagecontract.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         path,
		DraftDocument:     json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		BlueprintID:       strings.TrimSpace(req.BlueprintID),
	}); err != nil {
		logger.Scene("page").With("projectId", projectID).With("path", path).Error(err, "创建页面失败")
		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	pageListJump(c, true, pageCreatedMsg())
}

// DeletePage 单条删除页面（POST /admin/pages/delete）。
//
// 语义**逐字复用** API 的 /api/page/delete（同一个 svc.Delete）：页面有已激活产物时
// 不拒绝，而是先按 active 路径把访问面下线、清掉路由占用与媒体引用，再软删页面
// （见 service/page_delete.go）。批量删除必须走这同一条路径 —— 单条拒绝 / 批量跳过的
// 规则都由 service 决定，handler 不另立一套。
func (h *pagesAdminHandle) DeletePage(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageDeleteFormReq](c)
	if !ok {
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		pageListJump(c, false, pagesLocalNoticeMissingID)
		return
	}
	if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("pageId", id).Error(err, "删除页面失败")
		// 页面路径的文案出口：业务 sentinel 翻成中文，其余落归口文案（原文只进日志）。
		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	pageListJump(c, true, pageDeletedMsg())
}

// PagesBulkDelete 批量删除页面（POST /admin/pages/bulk-delete）。
//
// 逐条走同一条单条删除路径：某一条失败（已不存在、路径清理失败等）只计入跳过数，
// 整批不中断 —— 整批回滚会让用户以为「一个都没删」，然后反复重试。
// 结果按「已删除 N 个 / 跳过 M 个」渲染进整页提示，不静默部分成功。
//
// 有跳过（或一条都没处理）走失败档（OK:false、不自动跳）：运营要看清剩下哪些没删掉；
// 全部成功才 1 秒后自动回列表页。
func (h *pagesAdminHandle) PagesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）走 shell 的受控文案出口（类型判定，不认文本）。
		pageListJump(c, false, pageBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
			logger.Scene("page").With("pageId", id).Error(err, "批量删除页面失败")
			skipped++
			continue
		}
		deleted++
	}
	pageListJump(c, skipped == 0 && deleted > 0, outcomeMsg(pageenums.PagesBulkDeleteOutcome(deleted, skipped)))
}

// —— 本功能自己的回执文案（key + 中文兜底；key 与原文只有这一份）——
//
// 落在列表页文件里而不是 page_jump.go：这些回执只服务列表页的三个写动作
// （新建工程 / 新建页面 / 单删与批删），回跳目标也正好是这一页里的东西
// （pageListJump 与 pageListBack 留在 page_jump.go 是因为语言面板、排定面板、
// 翻译工作台写的也都是列表页内的东西，那份回跳是共用的）。

// pageProjectCreatedMsg 新建站点工程的成功回执（复用 project 模块的 MsgProjectCreated 词条）。
func pageProjectCreatedMsg() pageMsg {
	return pageMsg{Key: projectenums.MsgProjectCreated, Fallback: "站点工程创建成功"}
}

// pageCreatedMsg 新建页面的成功回执。
func pageCreatedMsg() pageMsg {
	return pageMsg{Key: pageenums.MsgPageCreated, Fallback: "页面创建成功"}
}

// pageDeletedMsg 单条删除页面的成功回执（与批量删除同一句话，只是计数为 1）。
func pageDeletedMsg() pageMsg {
	return outcomeMsg(pageenums.PagesBulkDeleteOutcome(1, 0))
}

var (
	// 页面管理页自造、可原样展示的回执。
	pagesLocalNoticeMissingID = pageMsg{Key: pageenums.BulkPageMissingID, Fallback: "缺少页面 id，未执行删除。"}

	// pagesLocalNoticeProjectNameRequired 新建站点工程时名称为空
	// （POST /admin/projects/create 的 name 为空）。
	//
	// 为什么不复用通用词条 MsgFieldRequired：这条回执要说清「哪一条没填」。
	// 通用句「必填字段不能为空」在页面上等于什么都没说 —— 用户要自己把抽屉再开一遍才知道。
	pagesLocalNoticeProjectNameRequired = pageMsg{Key: pageenums.PageFormProjectNameRequired, Fallback: "站点工程名称不能为空，未创建。"}

	// pagesLocalNoticePathRequired 新建页面时路径为空（POST /admin/pages/create 的 draftPath 为空）。
	//
	// 缺工程 id **不走**这一条：那里复用 pageenums.ErrProjectRequired（403 已登记中英词条，
	// 语义就是「没有工程作用域」）。两条分开报的判据与 project 域主题页一致 ——
	// 合成句「项目与页面路径不能为空」把两件事说成同一件事，而修法完全不同：
	// 缺工程是选择器 / 工程列表的问题，缺路径是输入框的问题。
	pagesLocalNoticePathRequired = pageMsg{Key: pageenums.PageFormPathRequired, Fallback: "页面路径不能为空，未创建。"}
)

// pageBulkIDsMsg 批量 id 超限的**事实**（提示页用）。
//
// 判据是类型（*bulkids.IDsError）而不是文案比对：两个数字（上限 Max 与去重后的本次条数 Count）
// 只有错误类型知道，调用方用 bulkids.MaxIDs 重算会丢掉 Count。
func pageBulkIDsMsg(c *gin.Context, err error) pageMsg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		// 不是本模块认识的错误：原文只进日志，对外归口。
		logger.Scene(pageErrScene).With("path", c.Request.URL.Path).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		return pageInternalMsg()
	}
	logger.Scene(pageErrScene).With("path", c.Request.URL.Path).
		With("count", tooMany.Count).With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return pageMsg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

// === 接口面（JSON API：/api/page/*）===
// Handle page 模块 HTTP 处理器。
type Handle struct {
	svc pagecontract.PageService
}

// NewHandle 创建 page HTTP 处理器。
func NewHandle(svc pagecontract.PageService) *Handle {
	return &Handle{svc: svc}
}

// Create 创建 Page、初始 Draft 和初始 Revision。
func (h *Handle) Create(c *gin.Context) {
	req, ok := binding.JSON[pagedto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageCreated, res)
}

// Detail 查询 Page 当前草稿。
func (h *Handle) Detail(c *gin.Context) {
	req, ok := binding.Query[pagedto.DetailReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

// List 列出全部页面摘要。
func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Query[pagedto.ListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// SaveDraft 保存完整 Page AST 并追加不可变 Revision。
func (h *Handle) SaveDraft(c *gin.Context) {
	req, ok := binding.JSON[pagedto.SaveDraftReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgDraftSaved, res)
}

// Build 基于当前草稿构建并暂存产物。
func (h *Handle) Build(c *gin.Context) {
	req, ok := binding.JSON[pagedto.BuildReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Build(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgBuildReady, res)
}

// Publish 激活暂存产物；allLangs=true 时一键发布全部启用语言（按语言逐条聚合结果）。
func (h *Handle) Publish(c *gin.Context) {
	req, ok := binding.JSON[pagedto.PublishReq](c)
	if !ok {
		return
	}
	if req.AllLangs {
		// 一键发布全部启用语言：进了编排就返回 200，失败明细按语言逐条在
		// results 里（单语言失败不阻断其余语言，与 rebuildPage 同一口径）。
		res, err := h.svc.PublishAllLanguages(c.Request.Context(), req)
		if err != nil {
			response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
			return
		}
		response.SuccessWithMessage(c, pageenums.MsgPublished, res)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPublished, res)
}

// RebuildArtifact 按产物元数据重建丢失的产物文件（灾难恢复）。
func (h *Handle) RebuildArtifact(c *gin.Context) {
	req, ok := binding.JSON[pagedto.RebuildArtifactReq](c)
	if !ok {
		return
	}
	res, err := h.svc.RebuildArtifact(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// GarbageCollectArtifacts 回收超期且无引用的产物文件（默认 dryRun，先看再删）。
func (h *Handle) GarbageCollectArtifacts(c *gin.Context) {
	req, ok := binding.JSON[pagedto.GCArtifactsReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GarbageCollectArtifacts(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// SEOPatrol 站点 SEO 合规巡检（审计 SEO-01）：只读，按激活清单逐份校验产物字节，
// 返回 URL / 规则 / 证据 / ArtifactHash 齐备的报告。
func (h *Handle) SEOPatrol(c *gin.Context) {
	req, ok := binding.Query[pagedto.SEOPatrolReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SEOPatrol(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// AuditPublication 巡检激活面：返回所有悬空/异常的激活链接。
func (h *Handle) AuditPublication(c *gin.Context) {
	res, err := h.svc.AuditPublication(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.Success(c, res)
}

// Rollback 回滚到历史产物。
func (h *Handle) Rollback(c *gin.Context) {
	req, ok := binding.JSON[pagedto.RollbackReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Rollback(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRollbackDone, res)
}

// UpdateURL 修改访问路径，旧路径 301 或取消激活。
func (h *Handle) UpdateURL(c *gin.Context) {
	req, ok := binding.JSON[pagedto.UpdateURLReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateURL(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgURLUpdated, res)
}

// Delete 软删页面并释放其全部路径占用。
func (h *Handle) Delete(c *gin.Context) {
	req, ok := binding.JSON[pagedto.DeleteReq](c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDeleted, nil)
}

// ListRevisions 查询 Page 草稿修订历史。
func (h *Handle) ListRevisions(c *gin.Context) {
	req, ok := binding.Query[pagedto.RevisionReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListRevisions(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRevisionsListed, res)
}

// pageErrorStatus 把 page service 返回的错误分类映射为 HTTP 状态码。
// 修复前按 err.Error() 中文文案 strings.Contains 匹配（文案改动即失效）；
// 修复后基于本包 sentinel error 精确 errors.Is 判定（文案与 pageenums 一致）。
// ErrInvalidParam（nil/空 ID 等参数错误）映射 400，与资源不存在（404）区分。
func pageErrorStatus(err error) int {
	switch {
	case errors.Is(err, pageservice.ErrNoStagedArtifact):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrPageNotFound),
		errors.Is(err, pageservice.ErrProjectNotFound),
		errors.Is(err, pageservice.ErrRollbackTargetMiss):
		return http.StatusNotFound
	case errors.Is(err, pageservice.ErrDraftVersionConflict),
		errors.Is(err, pageservice.ErrPathOccupied),
		errors.Is(err, pageservice.ErrRebuildRequired),
		// 定时上下线（PIPE-7）：正在执行的排定不能被取消 —— 与「草稿已变更」同属
		// 「当前状态与请求冲突，刷新后重试」，而不是调用方写错了参数。
		errors.Is(err, pageservice.ErrScheduleOccupied),
		errors.Is(err, pageservice.ErrScheduleRunning):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrScheduleNotFound):
		return http.StatusNotFound
	// 页面级语言排除（迁移 491）：「这一页不产出该语言」/「默认语言不能排除」都是
	// **当前状态与请求冲突**（刷新后能看到真实状态），不是调用方写错参数，
	// 也不是内部故障 —— 归到 409 与排定占用同一档，前端据此给"可展示的业务文案"。
	case errors.Is(err, pageservice.ErrPageLangExcluded),
		errors.Is(err, pageservice.ErrCannotExcludeDefaultLang),
		errors.Is(err, pageservice.ErrLangAlreadyExcluded),
		errors.Is(err, pageservice.ErrLangNotExcluded):
		return http.StatusConflict
	case errors.Is(err, pageservice.ErrInvalidParam),
		errors.Is(err, pageservice.ErrInvalidKind),
		errors.Is(err, pageservice.ErrInvalidDocument),
		errors.Is(err, pageservice.ErrInvalidPath),
		// DB-009 第三批：跨工程扇出入口在「工程表读不到 / 一个工程都没有」时显式失败，
		// 这是调用环境的问题（400）而不是内部故障（500）—— 它不该被兜底文案吞掉。
		errors.Is(err, pageservice.ErrProjectRequired),
		// 定时上下线（PIPE-7）：排定时间在过去 / 动作不在白名单内都属于请求本身不合法。
		errors.Is(err, pageservice.ErrScheduleInPast),
		errors.Is(err, pageservice.ErrScheduleActionInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// pageErrorMessage 把 service 错误映射为 JSON 出口的响应消息。
//
// 判据与页面出口**同一份**（adminui.PageErrorFact → pkg/response.IsBusinessError），
// 所以同一个错误在 JSON 与页面两条路上给出同一个结论；不再各维护一份白名单。
// 未命中（含 err == nil）时给的是**归口 key**（pageenums.MsgInternalError）而不是成品文案 ——
// JSON 层（response.ErrorWithMessage）会按当前语言取词；原文只进日志（PageErrorFact 已记，
// 带 path / user_id / 原始错误），避免 SQL 错误、连接信息这类内部细节泄露给客户端。
//
// 「key：明细」的明细段按 ErrorDetail 协议取词（pageDetailText），不是登记词条就丢弃并落日志 ——
// 改造前那条明细是 builder 的中文原文，英文界面上必然中英混排。
func pageErrorMessage(c *gin.Context, err error) string {
	fact := adminui.PageErrorFact(c, pageErrScene, err)
	if !fact.Business {
		return pageenums.MsgInternalError
	}
	text := adminui.TranslateArgs(c, fact.Key, fact.Fallback, nil)
	if fact.Tail != "" {
		if detail := pageDetailText(shell.TranslateFor(c), fact.Tail); detail != "" {
			return text + "：" + detail
		}
	}
	return text
}

// === 版本历史（页面本体的一段）===
// 背景：workbench.js 的 loadHistory 用 DOM 拼列表 + 每行绑恢复事件。
// 本文件把列表渲染搬到服务端；恢复动作也服务端化（查修订 → 覆盖保存草稿），
// 客户端只需确认与整页刷新。
//
// 端点（挂装配层传入的 /workbench 根级页面组）：
//
//	POST /workbench/history         → 修订列表片段
//	POST /workbench/history/restore → 恢复指定版本（覆盖草稿）
//
// HistoryPanel 渲染修订历史列表片段。
func (h *pagesAdminHandle) HistoryPanel(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageHistoryRestoreFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	// Revisions 用空切片而非 nil：Jet 的 len() 不接受 nil（会渲染失败）。
	// Error 交的是**事实**（key + 中文兜底），取词在片段模板里做 —— 片段不经
	// adminui.Prepare，所以必须显式补 t / tf（漏了不会报错：模板取到 nil，整片取词变空串）。
	data := adminui.PrepareBare(c, gin.H{"Revisions": []pagedto.HistoryRowResp{}, "Error": pageMsg{}})
	if pageID != "" && h.pages != nil {
		revs, err := h.pages.ListRevisions(c.Request.Context(), &pagecontract.RevisionReq{PageID: pageID})
		if err != nil {
			data["Error"] = pageMsg{Key: pageenums.HistoryLoadFailed, Fallback: "加载失败"}
		} else {
			views := make([]pagedto.HistoryRowResp, 0, len(revs))
			for _, r := range revs {
				views = append(views, pagedto.HistoryRowResp{
					Version: r.Version, Path: r.DraftPath,
					CreatedAt: r.CreatedAt,
				})
			}
			data["Revisions"] = views
		}
	}
	c.HTML(http.StatusOK, "fragments/history_list", data)
}

// HistoryRestore 恢复指定修订到草稿（覆盖保存为新修订，由客户端随后整页刷新）。
func (h *pagesAdminHandle) HistoryRestore(c *gin.Context) {
	ctx := c.Request.Context()
	req, ok := binding.Form[pagedto.PageHistoryRestoreFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	version, err := strconv.ParseInt(strings.TrimSpace(req.Version), 10, 64)
	if pageID == "" || err != nil || h.pages == nil {
		// 消息一律用模块 enums 的 key（pkg/response 按请求语言取词，中英各一条词条），
		// 不写字面量中文 —— 这里硬编码过「参数错误」，英文后台下是唯一的中文出口。
		response.ErrorWithMessage(c, http.StatusBadRequest, pageenums.ErrInvalidParam)
		return
	}
	revs, err := h.pages.ListRevisions(ctx, &pagecontract.RevisionReq{PageID: pageID})
	if err != nil {
		logger.Scene("page").With("page_id", pageID).Error(err, "读取页面修订失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, adminui.MsgInternalError)
		return
	}
	var target *pagecontract.RevisionResp
	for i := range revs {
		if revs[i].Version == version {
			target = &revs[i]
			break
		}
	}
	if target == nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pageenums.ErrRevisionNotFound)
		return
	}
	page, err := h.pageOf(c, pageID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pageenums.ErrPageNotFound)
		return
	}
	res, err := h.pages.SaveDraft(ctx, &pagecontract.SaveDraftReq{
		ID: pageID, ExpectedVersion: page.DraftVersion,
		DraftPath: target.DraftPath, DraftDocument: target.DraftDocument,
	})
	if err != nil {
		// 这里是「用某个修订覆盖保存草稿」：最常见的失败是乐观锁冲突（另一个标签页刚存过），
		// 与原先的 409 语义一致；错误原文只进日志，不再拼进响应体。
		logger.Scene("page").With("page_id", pageID).Error(err, "恢复页面修订失败")
		response.ErrorWithMessage(c, http.StatusConflict, pageenums.ErrDraftVersionConflict)
		return
	}
	response.Success(c, gin.H{"draftVersion": res.DraftVersion})
}
