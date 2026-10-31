package analyticshttp

import (
	"go_wp/internal/middleware/builtin"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const analyticsPageTitle = "访问统计"

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

func (h *analyticsPageHandle) AnalyticsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)

	loadFailed := err != nil
	pageErr := ""
	if loadFailed {
		logger.Scene("analytics").
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "读取站点工程列表失败")
		pageErr = analyticsInternalText(c)
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

	view := &analyticsdto.AnalyticsPageResp{
		Title:           analyticsPageTitle,
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
		Err:                  pageErr,

		LoadFailed: loadFailed,
	}

	base := reqparams.FilterBaseURL("/admin/analytics", map[string]string{
		"project": selected, "from": from, "to": to, "view": analyticsViewPaths,
	})

	_ = adminui.AttachPaginationView(view, "", nil,
		pagination.Page{Total: pathTotal, Page: page, Limit: limit, Base: base})

	pageview.Render(c, view)
}

func breakdownUnavailable(res *analyticscontract.SummaryResp) bool {
	if res == nil || res.Total <= 0 {
		return false
	}
	if res.BreakdownSource != analyticsdto.SourceDetail {
		return false
	}
	return len(res.Referrers)+len(res.UAClasses)+len(res.Langs) == 0
}

const analyticsInternalFallback = "统计服务内部错误，请稍后重试"

func analyticsInternalText(c *gin.Context) string {
	return i18n.TranslateFunc(response.RequestLanguage(c))(analyticsenums.ErrAnalyticsInternal, analyticsInternalFallback)
}

func analyticsFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		logger.Scene("analytics").Error(err, "访问统计查询失败")
		return analyticsInternalText(c)
	}

	text := i18n.TranslateFunc(response.RequestLanguage(c))(fact.Key, fact.Fallback)
	if fact.Tail != "" {
		text += ": " + fact.Tail
	}
	return text
}

const seoPageTitle = "SEO 控制台"

type seoPageHandle struct {
	analytics analyticscontract.AnalyticsService
	projects  projectcontract.ProjectService
}

func NewSEOPageHandle(analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) *seoPageHandle {
	return &seoPageHandle{analytics: analytics, projects: projects}
}

func (h *seoPageHandle) SEOPage(c *gin.Context) {
	view := &analyticsdto.SEOPageResp{
		Title:           seoPageTitle,
		Menu:            "seo",
		Projects:        []projectcontract.ProjectResp{},
		SelectedProject: "",
		Paths:           []analyticsdto.PathCount{},
		HasPaths:        false,
		AnalyticsError:  false,
	}

	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		logger.Error(err, "SEO 控制台读取站点列表失败")
		view.ProjectError = true
		pageview.Render(c, view)
		return
	}
	view.Projects = projects

	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	view.SelectedProject = selected

	if selected != "" && h.analytics != nil {
		summary, summaryErr := h.analytics.Summary(c.Request.Context(), &analyticscontract.SummaryReq{
			ProjectID: selected,
			PathPage:  1,
			PathLimit: 10,
		})
		if summaryErr != nil {
			logger.Error(summaryErr, "SEO 控制台读取热门路径失败，站点："+selected)
			view.AnalyticsError = true
		} else if summary != nil {
			view.Paths = summary.Paths
			view.HasPaths = len(summary.Paths) > 0
		}
	}

	pageview.Render(c, view)
}
