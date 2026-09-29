package pagehttp

// 页面管理列表页（后台「页面」入口）：列出/新建站点工程与页面，
// 行内直达可视化工作台。交互遵循后台 HTMX 规范：HTMX 请求返回
// Jet 片段，否则完整页面/重定向。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

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
	// page_translations_index.go）。
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
	Pages    []pageRow
	// Blueprints 蓝图候选（审计 VIS-010）：「从蓝图开始」是新建页面流程里的一个选项，
	// 不是另一个需要先去的页面。
	Blueprints []blueprintOption
	// Err / Done 是列表页回带的操作结论（?err= / ?done=）：单条删除与批量删除共用这一对键。
	// 批量结果按「已删除 N 个页面 / 跳过 M 个」写进 Done（有跳过时写 Err，警告条更显眼）。
	Err  string
	Done string

	// 发布回执收敛的只读观测（本轮接入）：待收敛条数 / 最老一条已等待多久 / 本进程最近一次
	// 收敛时刻。列表页是运维每天的落点，积压只写在日志与 /readyz 里等于不可见 ——
	// /readyz 又不参与就绪判定，没有人会因为它去看。
	//
	// ReceiptAlert 由条数派生（> 0），模板据此在「警告条」与「正常」之间分流：
	// 把判断留在 Go 侧，模板不必为 int64 与字面量的类型匹配操心（Jet 的比较要求同型）。
	ReceiptPending      int64
	ReceiptOldest       string
	ReceiptLastConverge string
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
		"Done":       d.Done,

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

// pageRow 列表行投影（含状态文案）。
type pageRow struct {
	ID        string
	ProjectID string
	Kind      string
	DraftPath string
	Active    bool
	Staged    bool
	Stale     bool
	Version   int64
	UpdatedAt string

	// 定时上下线（PIPE-7）的行内投影：有待执行排定时显示「已排定 + 到点时刻」，
	// 有失败排定时显示「排定失败 + 原因」。
	//
	// 判据与文案都在 Go 侧算好（模板是磁盘热读文件，判据写进模板会随两边不同步而漂移）：
	// 时间已按**站点时区**格式化，失败原因已按当前语言取词 —— 模板只输出这两个字符串。
	SchedulePendingAt  string
	ScheduleFailedNote string
	// ScheduleAlert 是否有需要运营看一眼的失败排定（模板据此选徽标样式）。
	ScheduleAlert bool
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
			// 装载失败优先于 ?err=：它是**这次请求**真实发生的事，
			// URL 里那条是上一次写失败留下的旧提示。
			Err: pageErrPageText(c, err),
			// ReceiptKnown 留 false：这次没读到回执状态，模板不该报「收敛正常」。
		}
	}
	c.HTML(http.StatusOK, "admin/page/pages", shell.Prepare(c, data.templateMap()))
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
	schedules := map[string]pagecontract.SchedulePageSummary{}
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
	rows := make([]pageRow, 0, len(pages))
	for _, p := range pages {
		row := pageRow{
			ID: p.ID, ProjectID: p.ProjectID, Kind: p.Kind,
			DraftPath: p.DraftPath, Active: p.ActiveArtifactID != nil,
			Staged: p.StagedArtifactID != nil, Stale: p.Stale,
			Version: p.DraftVersion, UpdatedAt: p.UpdatedAt.Time().Format("2006-01-02 15:04"),
		}
		if summary, ok := schedules[p.ID]; ok {
			row.SchedulePendingAt = scheduleRowTime(summary.Pending)
			if summary.Failed != nil {
				row.ScheduleAlert = true
				row.ScheduleFailedNote = scheduleFailureText(shell.TranslateFor(c), summary.Failed.LastError)
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
		// 操作结论走 query 回带（PRG）：单条删除与批量删除共用这一对键，
		// 页面本身不做筛选，故回跳不带其它参数。
		// 读侧一律经 page_err.go 的白名单出口（查询参数不是可信边界）。
		Err:  pagePageErr(c),
		Done: pagePageDone(c),

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

// receiptBacklogObserver 收敛积压的只读观测（page service 实现）。
//
// 用隐式接口而不是扩 pagecontract：可观测不是跨模块能力（与 routers 侧的
// pendingReceiptBacklog 同一判据），契约扩一次会让所有测试替身跟着实现一遍，
// 而这只服务列表页上的一个状态条。
type receiptBacklogObserver interface {
	PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error)
}

// receiptBacklog 读取待收敛回执的观测值，转成可直接渲染的三元文本。
//
// 未实现观测（测试替身 / 降级装配）与读取失败都返回零值：状态条退化成「正常」那一支，
// 列表页本身不受影响。失败只记日志 —— 这是观测，不是页面数据
// （原文不进模板，见 AGENTS.md 的错误文案三件套）。
func (h *pagesAdminHandle) receiptBacklog(ctx context.Context) (pending int64, oldest, last string) {
	observer, ok := h.pages.(receiptBacklogObserver)
	if !ok {
		return 0, "", ""
	}
	count, oldestAge, lastConvergeAt, err := observer.PendingReceiptBacklog(ctx)
	if err != nil {
		logger.Scene("page").With("err", err).Warn("读取待收敛发布回执失败（列表页不显示回执状态）")
		return 0, "", ""
	}
	if count > 0 && oldestAge > 0 {
		oldest = formatReceiptAge(oldestAge)
	}
	if !lastConvergeAt.IsZero() {
		last = lastConvergeAt.Local().Format("2006-01-02 15:04:05")
	}
	return count, oldest, last
}

// formatReceiptAge 把等待时长压成人读的一行（秒 / 分 / 小时 / 天）。
//
// 不做 i18n：单位是 SI 记法（s/m/h/d），中英文都读得懂，也不需要复数规则 ——
// 为它造四条词条只会让词条表更长，不带来任何可读性。
func formatReceiptAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// staleOverviewLimit 「全站待重建」区块一次列出的页面数。
//
// 这是**消费者口径**，所以定义在调用方：ListStalePages 的 limit 由调用方给
// （service 不写死业务条件），不填时才落到 model 的 DefaultStaleListLimit = 50 ——
// 一份 50 行的折叠清单会把页面列表顶出首屏，而那正是审计 02-L P1-10 记录的原缺陷
// （只读影响面卡占了列表主位）。
//
// 数为什么是 8：只读区块的作用是「让人看见影响面」，不是完整清单；被截断的条数由 Total
// 给出并在页面上显式说明。service 里那份同名同值的未导出常量已随之删除（它没有任何调用方，
// 留着会让「这个数由谁定」出现两个答案）。
const staleOverviewLimit = 8

// staleOverviewItem 「全站待重建」清单的一行。
//
// 跨工程清单必须带工程名：每个工程都可能有一个一模一样的 /about，只给路径分不清是哪一个
// （页面列表页本身有「所属工程」上下文，这一块没有）。
type staleOverviewItem struct {
	ID          string
	Path        string
	ProjectName string
	// Published 是否已上线（有活跃产物路径）：用来区分「已发布但有更新未发布」与「从未上线」——
	// 后者的处置方式不同（重建也还不会出现在访问面，要先发布）。
	Published bool
}

// staleOverview 取「全站待重建」区块的数据（只读观测）。
//
// 作用域：ProjectID 传空 = **全部站点工程**。这与本页下方列表的口径不同 —— 列表是单工程聚焦
// （focusProjectID 从 ?project= 解析），而块 / 文章 / 主题 / 词条改动影响的是全站。
// 两个数不是同一个，模板侧把「全站」写进标题与说明。
//
// 失败不降级成空清单：ListStalePages 的语义是「读不到即失败」，这里把它翻成 Available=false
// （模板显示「本次读不到」），**绝不渲染成「0 个待重建」**—— 「影响面 0」与「读不到影响面」
// 混在一起，会让一次读取失败在页面上看起来像一切正常，运维再也不会去看
// （判据与发布回执观测的 ReceiptKnown 一致）。原文只进日志。
//
// 唯一的例外是「一个站点工程都没有」：ListStalePages 按语义返回 ErrProjectRequired
// （没有可作用域的工程），而那时全站确实没有任何页面 —— 那是确定的事实，不是读取失败，
// 按空态处理（此时下方列表也正落在「还没有页面」那一档）。
func (h *pagesAdminHandle) staleOverview(ctx context.Context) gin.H {
	empty := gin.H{
		"Available": true, "Total": 0, "Pages": []staleOverviewItem{},
		// Limit 给真实口径（模板在空态下不读它，但零值会让「清单上限是多少」在两个分支里
		// 出现两种答案 —— 将来若空态也要说一句「最多列 8 条」，零值就是错的）。
		"Limit": staleOverviewLimit, "Truncated": false,
	}
	unavailable := gin.H{
		"Available": false, "Total": 0, "Pages": []staleOverviewItem{},
		"Limit": 0, "Truncated": false,
	}
	if h == nil || h.pages == nil {
		// 契约未注入（降级装配、或只覆盖写路径的测试句柄）：与「读不到」同形，
		// 但没有错误可记，静默给失败态即可。
		return unavailable
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		// 条数由本页给（不填会落到 model 的 50 条默认），排序按标记时间倒序：
		// 「最近这次改动影响的」排在最前（service 文件头对这个消费者的描述就是这个次序）。
		Limit:      staleOverviewLimit,
		Descending: true,
	})
	if err != nil {
		if errors.Is(err, pageservice.ErrProjectRequired) {
			return empty
		}
		logger.Scene("page").With("err", err).Warn("读取全站待重建清单失败（页面列表页的该区块显示为不可用）")
		return unavailable
	}
	if res == nil {
		// 契约返回 (nil, nil) 是异常形态：按「读不到」处理，不给「0 个待重建」的假结论。
		logger.Scene("page").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable
	}
	items := make([]staleOverviewItem, 0, len(res.Pages))
	for i := range res.Pages {
		items = append(items, staleOverviewItem{
			ID:          res.Pages[i].ID,
			Path:        strings.TrimSpace(res.Pages[i].Path),
			ProjectName: strings.TrimSpace(res.Pages[i].ProjectName),
			Published:   res.Pages[i].Published,
		})
	}
	return gin.H{
		"Available": true,
		"Total":     res.Total,
		"Pages":     items,
		"Limit":     res.Limit,
		"Truncated": res.Truncated,
	}
}

// CreateProject 新建站点工程。
//
// 出口形态由**表单怎么提交**决定，不由 handler 的注释决定：表单是原生
// `<form method="post" action="/admin/projects/create">`（admin/pages.html 的
// #tpl-project-create 抽屉，不是 hx-post），成功与失败都走 PRG ——
// 成功的 303 回列表页刷出新工程，失败的 303 回同一个列表页 + ?err=<当前语言文案>
// （读侧 pagePageErr 白名单，页面顶部渲染成提示条）。
//
// 为什么不是 `c.String(400, …)`：那会把用户导航到一块只有一行字的页面上，
// 抽屉、页壳、他刚填的名称一并丢失；也不是 400 + 片段（那是给 htmx 请求准备的形态，
// 原生表单收到片段会把 JSON / HTML 片段当成新页面渲染）。
func (h *pagesAdminHandle) CreateProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageBulkTextOf(c, pagesLocalNoticeProjectNameRequired), ""))
		return
	}
	if _, err := h.projects.Create(c.Request.Context(), &projectcontract.CreateReq{
		Name: name, Settings: json.RawMessage("{}"),
	}); err != nil {
		logger.Scene("page").With("name", name).Error(err, "创建站点工程失败")
		// 页面路径的文案出口：业务 sentinel 翻成中文，其余落归口文案
		//（原文只进日志 —— 上面那条日志已带 name，这里用不记日志的变体，免得同一错误记两遍）。
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageFacingOrInternal(c, err), ""))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/pages")
}

// CreatePage 新建页面（默认空白草稿，创建后可进工作台编辑）。
//
// 出口形态同 CreateProject：原生表单 + PRG，失败 303 回列表页 + ?err=。
func (h *pagesAdminHandle) CreatePage(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	path := strings.TrimSpace(c.PostForm("draftPath"))
	// 两条必填分开报，不合成一句「项目与页面路径不能为空」：合成句把「没选工程」
	// 与「没填路径」说成同一件事，而两者的修法完全不同（选择器 vs 输入框）。
	if projectID == "" {
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageFacingKey(c, pageenums.ErrProjectRequired), ""))
		return
	}
	if path == "" {
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageBulkTextOf(c, pagesLocalNoticePathRequired), ""))
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
		BlueprintID:       strings.TrimSpace(c.PostForm("blueprintId")),
	}); err != nil {
		logger.Scene("page").With("projectId", projectID).With("path", path).Error(err, "创建页面失败")
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageFacingOrInternal(c, err), ""))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/pages")
}

// DeletePage 单条删除页面（POST /admin/pages/delete）。
//
// 语义**逐字复用** API 的 /api/page/delete（同一个 svc.Delete）：页面有已激活产物时
// 不拒绝，而是先按 active 路径把访问面下线、清掉路由占用与媒体引用，再软删页面
// （见 service/page_delete.go）。批量删除必须走这同一条路径 —— 单条拒绝 / 批量跳过的
// 规则都由 service 决定，handler 不另立一套。
func (h *pagesAdminHandle) DeletePage(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageBulkTextOf(c, pagesLocalNoticeMissingID), ""))
		return
	}
	if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("pageId", id).Error(err, "删除页面失败")
		// 页面路径的文案出口：业务 sentinel 翻成中文，其余落归口文案（原文只进日志）。
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageFacingOrInternal(c, err), ""))
		return
	}
	c.Redirect(http.StatusSeeOther, pagesBackURL("", fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[1]), strconv.Itoa(1))))
}

// PagesBulkDelete 批量删除页面（POST /admin/pages/bulk-delete）。
//
// 逐条走同一条单条删除路径：某一条失败（已不存在、路径清理失败等）只计入跳过数，
// 整批不中断 —— 整批回滚会让用户以为「一个都没删」，然后反复重试。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，不静默部分成功。
func (h *pagesAdminHandle) PagesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageErrPageText(c, berr), ""))
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
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	msg := pagesBulkDeleteResult(c, deleted, skipped)
	if skipped > 0 {
		c.Redirect(http.StatusSeeOther, pagesBackURL(msg, ""))
		return
	}
	c.Redirect(http.StatusSeeOther, pagesBackURL("", msg))
}

// pagesBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func pagesBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return pageBulkTextOf(c, pagesBulkResultTemplates[0])
	case skipped == 0:
		return fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[1]), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[2]), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[3]), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

// pagesBackURL 列表页回跳地址（PRG）。两条文案都由服务端拼装（受控文本 + 计数），
// 经 QueryEscape 回带；模板侧 Jet 默认 HTML 转义，不构成注入面。
func pagesBackURL(errText, doneText string) string {
	q := url.Values{}
	if errText != "" {
		q.Set("err", errText)
	}
	if doneText != "" {
		q.Set("done", doneText)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/pages?" + enc
	}
	return "/admin/pages"
}
