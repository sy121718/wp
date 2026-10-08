package analyticshttp

// 只读页面：按天 / 按路径 / 来源域 / 设备分类 / 语言五个维度的浏览数与独立访客数 + 总数 +
// 时间范围筛选 + 路径表分页。5 张同构的维度表以页签（tabs）呈现 —— **5 个面板都由服务端全部
// 渲染**，标签点击是纯前端切换；URL 的 ?view= 只决定初始选中的是哪一个（见 analyticsViewMode）。
// 没有写操作，所以不挂 CasbinMiddlewareForPath（那只用于写表单）；访问控制由菜单权限点
// （analytics:view）与只读 API 的 Casbin 策略承担。
//
// 一条纪律：**页面不做任何聚合计算**。所有数字都来自 analytics 模块的 Summary ——
// 页面自己再算一遍（或按天再累加得出总数）迟早会得出两个数，
// 而报表里两个数打架时，没人知道该信哪个。
//
// 取数只经本模块 contract 与 project 模块的 contract（跨模块只碰 contract + 不可变 dto）。

// SEO 控制台读 analytics + project 契约，与统计页同域相邻；SEO 体检动作由
// publication 现有端点执行，本页只编排已有只读契约。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/analytics/contract"
	"go_wp/internal/module/analytics/dto"
	"go_wp/internal/module/analytics/enums"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
)

// analyticsPageTitle 页面标题（模块 enums 没有这个键，走 shell 的 i18n fallback 链路）。
const analyticsPageTitle = "访问统计"

// 5 张同构维度表（列头都是「维度名 + 浏览数 + 独立访客」）收进 tabs 后的 5 个视图。
//
// URL 的 ?view= **只决定初始选中哪一个标签**：标签点击由 admin.js 纯前端切换
// （只改 aria-selected / tabindex / hidden，不改 URL）。因此「会重载页面的入口」
// （工程选择 / 时间范围表单 / 分页链接）必须各自把 view 带上，否则重载后回到第一个标签，
// 用户会以为自己的操作没生效 —— 路径表那个死角见 AnalyticsPage 里分页 base 的注释。
const (
	analyticsViewDaily     = "daily"     // 按天（默认视图）
	analyticsViewPaths     = "paths"     // 按路径（5 个视图里唯一带分页的）
	analyticsViewReferrers = "referrers" // 来源域
	analyticsViewUAClasses = "ua"        // 设备分类
	analyticsViewLangs     = "langs"     // 语言
)

// analyticsViewMode 读 ?view= 决定初始选中的维度视图；空值或不认识的值一律回默认视图。
//
// 不认识的值不当作错误：它只可能来自手改 URL，而页面数据与「选中哪个视图」无关
// （5 个面板本来就是全渲染的），为它中断整页没有任何收益。
func analyticsViewMode(c *gin.Context) string {
	switch strings.TrimSpace(c.Query("view")) {
	case analyticsViewPaths:
		return analyticsViewPaths
	case analyticsViewReferrers:
		return analyticsViewReferrers
	case analyticsViewUAClasses:
		return analyticsViewUAClasses
	case analyticsViewLangs:
		return analyticsViewLangs
	default:
		return analyticsViewDaily
	}
}

// analyticsFacingMessages 本页允许原样显示的**统计模块**文案白名单。
//
// 错误可能是任何东西（数据库连接断了、上下文超时），原样输出等于把内部细节摊给运营看；
// 而「时间范围不合法」这类要告诉用户具体哪里错了，所以走白名单：
// 认识的文案照原样显示，其余一律收敛为统一的内部错误提示并记日志。
var analyticsFacingMessages = []string{
	analyticsenums.ErrInvalidParam,
	analyticsenums.ErrInvalidRange,
}

// analyticsPageHandle 访问统计页处理器。
type analyticsPageHandle struct {
	analytics analyticscontract.AnalyticsService
	projects  projectcontract.ProjectService
}

// NewAnalyticsPageHandle 构造。
func NewAnalyticsPageHandle(analytics analyticscontract.AnalyticsService,
	projects projectcontract.ProjectService) *analyticsPageHandle {
	return &analyticsPageHandle{analytics: analytics, projects: projects}
}

// AnalyticsPage 访问统计页（GET /admin/analytics）。
func (h *analyticsPageHandle) AnalyticsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	// 工程列表装载失败：**降级渲染**，不拿走整个页面（与主题管理页、admin 六页同一判据）。
	//
	// 原先这里是 `c.String(500, shell.MsgInternalError)`：浏览器里没有页面，只有一块纯文本，
	// 而且那块文本是**未翻译的裸 key**（页面上直接显示 `MsgInternalError` 这串英文）——
	// `c.String` 不经过任何 translate，运营看到的不是人话，也没有侧栏 / 页头 / 时间范围筛选。
	//
	// 装载失败时不取数：selected 为空，Summary 要工程作用域，跑下去只会再报一次
	// 「未指定工程」，把「工程列表没读到」盖成「你没选工程」。列表留空，模板按 LoadFailed
	// 把「还没有站点工程」的空态换成「没读出来」—— 那句话在此时是错的（工程明明存在）。
	loadFailed := err != nil
	pageErr := ""
	if loadFailed {
		logger.Scene("analytics").
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "读取站点工程列表失败")
		pageErr = analyticsInternalText(c)
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	page, limit := shell.PageParams(c)
	viewMode := analyticsViewMode(c)

	var (
		total, visitors int64
		pathTotal       int64
		daily           []analyticscontract.DailyCount
		paths           []analyticscontract.PathCount
		// 来源域 / 设备分类 / 语言的排行（与 Paths 同源：全部由 Summary 给出）。
		referrers, uaClasses, langs []analyticscontract.RankCount
		rankLimit                   int
		rangeFrom, rangeTo          string
		// 维度分布不可用（明细被保留期清理、按天汇总仍在）：见 breakdownUnavailable。
		breakdownMissing bool
	)
	if selected != "" {
		res, serr := h.analytics.Summary(ctx, &analyticsdto.SummaryReq{
			ProjectID: selected, From: from, To: to, PathPage: page, PathLimit: limit,
		})
		if serr != nil {
			pageErr = analyticsFacingError(c, serr)
		} else if res != nil {
			total, visitors = res.Total, res.Visitors
			daily, paths, pathTotal = res.Daily, res.Paths, res.PathTotal
			referrers, uaClasses, langs = res.Referrers, res.UAClasses, res.Langs
			rankLimit = res.RankLimit
			rangeFrom, rangeTo = res.From, res.To
			breakdownMissing = breakdownUnavailable(res)
		}
	}

	data := shell.Prepare(c, gin.H{
		"title":           analyticsPageTitle,
		"menu":            "analytics",
		"Projects":        projects,
		"SelectedProject": selected,
		"FilterFrom":      from,
		"FilterTo":        to,
		"RangeFrom":       rangeFrom,
		"RangeTo":         rangeTo,
		"Total":           total,
		"Visitors":        visitors,
		"Daily":           daily,
		"Paths":           paths,
		"PathTotal":       pathTotal,
		"Referrers":       referrers,
		"UAClasses":       uaClasses,
		"Langs":           langs,
		"RankLimit":       rankLimit,
		// 初始选中的维度视图（tabs）：模板据此决定 5 个标签里哪个 aria-selected，以及哪个面板
		// 不加 hidden。模板对缺键有兜底（回落 daily），所以「别的渲染路径漏传该键」不会断页。
		"ViewMode": viewMode,
		// 只有「总数有数、维度榜全空」这一种情况给解释，见 breakdownUnavailable。
		"BreakdownUnavailable": breakdownMissing,
		"Err":                  pageErr,
		// LoadFailed：本次请求的工程列表没读到（不是「还没有工程」）。
		// 模板据此把空态换成「没读出来」—— 显示「还没有站点工程」会让运营去建工程，
		// 而真正的问题是这一次没读出来。
		"LoadFailed": loadFailed,
	})
	// 分页链接的 base **固定带 view=paths**，而不是当前的 viewMode —— 这是本页最容易漏的一处：
	// 分页条物理上只属于「按路径」面板（其余 4 个视图不分页）。若 base 跟着 viewMode 走，
	// 用户在「按天」标签下时路径面板是 hidden 的，翻页链接带的是 view=daily，点「下一页」
	// 重载后仍停在按天 —— 路径表在 hidden 面板里，表现就是「点了下一页什么也没发生」。
	// 固定成 paths 之后：从路径标签翻页 → 重载 → 仍停在路径视图，第 2 页可见
	// （回归见 analytics_page_render_test.go 的 view=paths&page=2 直达用例）。
	base := shell.FilterBaseURL("/admin/analytics", map[string]string{
		"project": selected, "from": from, "to": to, "view": analyticsViewPaths,
	})
	for k, v := range shell.BuildPagination(pathTotal, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/analytics/analytics.html", data)
}

// breakdownUnavailable 判断「维度分布不可用」—— 这时页面要给一句解释，而不是让运营看空表。
//
// 判据是「总数有数、三个维度榜全空」：三个维度榜读的是访问明细（page_views），
// 而按天汇总（page_views_daily）在明细被保留期清理之后仍然保留（清理只删明细）。
// 所以「总数与路径榜有值、维度榜全空」只可能来自这种状态；
// 真的没有流量时不在此列（那时的总数也是 0），不该提示。
//
// BreakdownSource == detail 这一项当前恒真（维度排行固定读明细，见 analytics 模块
// Summary 的形态选择），写出来是为了这个判断在将来维度改走预聚合时仍然正确 ——
// 那时它就不再是冗余条件了。
func breakdownUnavailable(res *analyticscontract.SummaryResp) bool {
	if res == nil || res.Total <= 0 {
		return false
	}
	if res.BreakdownSource != analyticsdto.SourceDetail {
		return false
	}
	return len(res.Referrers)+len(res.UAClasses)+len(res.Langs) == 0
}

// analyticsInternalFallback 归口文案的中文兜底（i18n 未初始化 / 该 key 尚无词条时用）。
//
// 有它就不会出现裸 key：i18n.Translate 的兜底链最后一条是「fallback 为空则返回 key 本身」，
// 所以只要有中文兜底，页面最差也显示一句人话。
const analyticsInternalFallback = "统计服务内部错误，请稍后重试"

// analyticsInternalText 统计页的归口文案：**当前语言**的译文（未命中词条回落中文）。
//
// 为什么不直接用 shell.MsgInternalError：它的**值**就是 key 本身（"MsgInternalError"），
// 而页面上的 {{.Err}} 是直接渲染的文本、不经过 response 的 translate —— 未命中白名单时
// 把它原样返回，页面上显示的就是这串英文。取词走 shell.TranslateFor 是这一层唯一的出口。
//
// 归口 key 取模块自己的 enums（ErrAnalyticsInternal）而不是壳层的通用 key：页面上写着
// 「统计服务内部错误」时，排障的人一眼知道该去看 analytics 的场景日志。
func analyticsInternalText(c *gin.Context) string {
	return shell.TranslateFor(c)(analyticsenums.ErrAnalyticsInternal, analyticsInternalFallback)
}

// analyticsFacingError 把统计错误映射为页面可显示的文案（白名单收口，见 analyticsFacingMessages）。
//
// 收 c 是因为两支都要取**当前语言**的译文：命中白名单的那一支要取业务文案，
// 未命中的那一支要取归口文案。只带 err 的话这一层就只能硬编码一句中文，
// 英文界面上会整块回落中文。
//
// **命中那一支必须取词**：analyticsenums 的值是 i18n item_key（ErrInvalidParam /
// ErrInvalidRange），而本页把返回值写进模板数据的 "Err"（:169）——{{.Err}} 是
// **直接渲染**的文本、不经过 pkg/response 的 translate。原样 return allowed 的话，
// 运营在页面上看到的是 `ErrInvalidRange` 这样的裸 key。fallback 给归口兜底文案：
// 词条缺失时页面显示一句人话，而不是把裸 key 摆出去。
func analyticsFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, allowed := range analyticsFacingMessages {
		if msg == allowed {
			return shell.TranslateFor(c)(allowed, analyticsInternalFallback)
		}
	}
	logger.Scene("analytics").Error(err, "访问统计查询失败")
	return analyticsInternalText(c)
}

const seoPageTitle = "SEO 控制台"

// seoPageHandle 只编排已有只读契约；SEO 体检由 publication 现有端点执行。
type seoPageHandle struct {
	analytics analyticscontract.AnalyticsService
	projects  projectcontract.ProjectService
}

// NewSEOPageHandle 创建 SEO 控制台处理器。
func NewSEOPageHandle(analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) *seoPageHandle {
	return &seoPageHandle{analytics: analytics, projects: projects}
}

// SEOPage 渲染当前站点的 SEO 体检入口与已有访问统计。
func (h *seoPageHandle) SEOPage(c *gin.Context) {
	data := gin.H{
		"title":           seoPageTitle,
		"menu":            "seo",
		"Projects":        []projectcontract.ProjectResp{},
		"SelectedProject": "",
		"Paths":           []analyticscontract.PathCount{},
		"HasPaths":        false,
		"AnalyticsError":  false,
	}

	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		logger.Error(err, "SEO 控制台读取站点列表失败")
		data["ProjectError"] = true
		c.HTML(http.StatusOK, "admin/analytics/seo.html", shell.Prepare(c, data))
		return
	}
	data["Projects"] = projects

	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	data["SelectedProject"] = selected

	if selected != "" && h.analytics != nil {
		summary, summaryErr := h.analytics.Summary(c.Request.Context(), &analyticscontract.SummaryReq{
			ProjectID: selected,
			PathPage:  1,
			PathLimit: 10,
		})
		if summaryErr != nil {
			logger.Error(summaryErr, "SEO 控制台读取热门路径失败，站点："+selected)
			data["AnalyticsError"] = true
		} else if summary != nil {
			data["Paths"] = summary.Paths
			data["HasPaths"] = len(summary.Paths) > 0
		}
	}

	c.HTML(http.StatusOK, "admin/analytics/seo.html", shell.Prepare(c, data))
}
