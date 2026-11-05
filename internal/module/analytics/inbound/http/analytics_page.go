package analyticshttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const (
	analyticsViewDaily     = "daily"
	analyticsViewPaths     = "paths"
	analyticsViewReferrers = "referrers"
	analyticsViewUAClasses = "ua"
	analyticsViewLangs     = "langs"
)

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

type analyticsPageHandle struct {
	analytics analyticscontract.AnalyticsService
	projects  projectcontract.ProjectService
}

func NewAnalyticsPageHandle(analytics analyticscontract.AnalyticsService,
	projects projectcontract.ProjectService) *analyticsPageHandle {
	return &analyticsPageHandle{analytics: analytics, projects: projects}
}

// AnalyticsPage 渲染访问统计页。
//
// 取数失败一律走统一出口 response.ErrorStatusAuto（真实状态码 + JSON），**不渲染失败页**：装配缺失
// → 500、无权限由路由中间件给 403、service 声明的参数错误给 400。页面只在成功时
// 出现，所以模板里没有「读取失败」提示条，也没有失败态布尔字段。
func (h *analyticsPageHandle) AnalyticsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		response.ErrorStatusAuto(c, analyticsErrScene, err)
		return
	}

	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	page, limit := reqparams.PageParams(c)
	viewMode := analyticsViewMode(c)

	var (
		total, visitors int64
		pathTotal       int64
		daily           []analyticscontract.DailyCount
		paths           []analyticscontract.PathCount

		referrers, uaClasses, langs []analyticscontract.RankCount
		rankLimit                   int
		rangeFrom, rangeTo          string

		breakdownMissing bool
	)
	if selected != "" {
		res, serr := h.analytics.Summary(ctx, &analyticsdto.SummaryReq{
			ProjectID: selected, From: from, To: to, PathPage: page, PathLimit: limit,
		})
		if serr != nil {
			response.ErrorStatusAuto(c, analyticsErrScene, serr)
			return
		}
		if res != nil {
			total, visitors = res.Total, res.Visitors
			daily, paths, pathTotal = res.Daily, res.Paths, res.PathTotal
			referrers, uaClasses, langs = res.Referrers, res.UAClasses, res.Langs
			rankLimit = res.RankLimit
			rangeFrom, rangeTo = res.From, res.To
			breakdownMissing = breakdownUnavailable(res)
		}
	}

	// 分页只交三个事实：总数 / 当前页 / 每页条数。窗口、Base 与每一页的链接全部
	// 由模板算 —— 公共 partial 用 pageBase(req_path, "page", "limit") 从当前请求
	// URI 推出 Base（project/from/to/view 原样保留），再用 pager() 出窗口。
	// 控制器既不拼 URL，也不给 Base。
	view := &analyticsdto.AnalyticsPageResp{
		Menu:            "analytics",
		Projects:        projects,
		SelectedProject: selected,
		FilterFrom:      from,
		FilterTo:        to,
		RangeFrom:       rangeFrom,
		RangeTo:         rangeTo,
		Total:           total,
		Visitors:        visitors,
		Daily:           daily,
		Paths:           paths,
		PathTotal:       pathTotal,
		Referrers:       referrers,
		UAClasses:       uaClasses,
		Langs:           langs,
		RankLimit:       rankLimit,

		ViewMode: viewMode,

		BreakdownUnavailable: breakdownMissing,

		Pagination: pagination.Page{Total: pathTotal, Page: page, Limit: limit},
	}

	pageview.Render(c, view)
}

// breakdownUnavailable 判的是「明细被保留期清理」这一数据完整性事实：
// 有流量、来源是明细、三张维度榜却都空 —— 不是读取失败。
func breakdownUnavailable(res *analyticscontract.SummaryResp) bool {
	if res == nil || res.Total <= 0 {
		return false
	}
	if res.BreakdownSource != analyticsdto.SourceDetail {
		return false
	}
	return len(res.Referrers)+len(res.UAClasses)+len(res.Langs) == 0
}

type seoPageHandle struct {
	analytics analyticscontract.AnalyticsService
	projects  projectcontract.ProjectService
}

func NewSEOPageHandle(analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) *seoPageHandle {
	return &seoPageHandle{analytics: analytics, projects: projects}
}

// SEOPage 渲染 SEO 控制台。取数失败同样走 JSON 早退：站点列表读不出来时页面
// 说什么都不对（既不是「还没有站点」，也不是「热门路径为空」），直接给真实状态。
func (h *seoPageHandle) SEOPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		response.ErrorStatusAuto(c, analyticsErrScene, err)
		return
	}

	view := &analyticsdto.SEOPageResp{
		Menu:     "seo",
		Projects: projects,
		Paths:    []analyticsdto.PathCount{},
	}

	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected

	if selected != "" && h.analytics != nil {
		summary, summaryErr := h.analytics.Summary(ctx, &analyticscontract.SummaryReq{
			ProjectID: selected,
			PathPage:  1,
			PathLimit: 10,
		})
		if summaryErr != nil {
			response.ErrorStatusAuto(c, analyticsErrScene, summaryErr)
			return
		}
		if summary != nil {
			view.Paths = summary.Paths
			view.HasPaths = len(summary.Paths) > 0
		}
	}

	pageview.Render(c, view)
}
