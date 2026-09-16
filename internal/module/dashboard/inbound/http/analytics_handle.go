// analytics_handle.go — 后台访问统计页（BIZ-8）。
//
// 只读页面：按天 / 按路径聚合的浏览数与独立访客数 + 总数 + 时间范围筛选 + 路径表分页。
// 没有写操作，所以不挂 CasbinMiddlewareForPath（那只用于写表单）；访问控制由菜单权限点
// （analytics:view）与只读 API 的 Casbin 策略承担。
//
// 一条纪律：**页面不做任何聚合计算**。所有数字都来自 analytics 模块的 Summary ——
// 页面自己再算一遍（或按天再累加得出总数）迟早会得出两个数，
// 而报表里两个数打架时，没人知道该信哪个。
package dashboardhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// analyticsPageTitle 页面标题（dashboard enums 没有这个键，走 withI18n 的 fallback 链路）。
const analyticsPageTitle = "访问统计"

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
	if err != nil {
		logger.Scene("analytics").Error(err, "读取站点工程列表失败")
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	page, limit := pageParams(c)

	var (
		total, visitors int64
		pathTotal       int64
		daily           []analyticscontract.DailyCount
		paths           []analyticscontract.PathCount
		// 来源域 / 设备分类 / 语言的排行（与 Paths 同源：全部由 Summary 给出）。
		referrers, uaClasses, langs []analyticscontract.RankCount
		rankLimit                   int
		rangeFrom, rangeTo          string
		pageErr                     string
		// 维度分布不可用（明细被保留期清理、按天汇总仍在）：见 breakdownUnavailable。
		breakdownMissing bool
	)
	if selected != "" {
		res, serr := h.analytics.Summary(ctx, &analyticsdto.SummaryReq{
			ProjectID: selected, From: from, To: to, PathPage: page, PathLimit: limit,
		})
		if serr != nil {
			pageErr = analyticsFacingError(serr)
		} else if res != nil {
			total, visitors = res.Total, res.Visitors
			daily, paths, pathTotal = res.Daily, res.Paths, res.PathTotal
			referrers, uaClasses, langs = res.Referrers, res.UAClasses, res.Langs
			rankLimit = res.RankLimit
			rangeFrom, rangeTo = res.From, res.To
			breakdownMissing = breakdownUnavailable(res)
		}
	}

	data := withCSRF(c, gin.H{
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
		// 只有「总数有数、维度榜全空」这一种情况给解释，见 breakdownUnavailable。
		"BreakdownUnavailable": breakdownMissing,
		"Err":                  pageErr,
	})
	base := filterBaseURL("/admin/analytics", map[string]string{
		"project": selected, "from": from, "to": to,
	})
	for k, v := range buildPagination(pathTotal, page, limit, base, translateFor(c)).templateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/analytics.html", data)
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

// analyticsFacingError 把统计错误映射为页面可显示的文案（白名单收口，见 analyticsFacingMessages）。
func analyticsFacingError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, allowed := range analyticsFacingMessages {
		if msg == allowed {
			return allowed
		}
	}
	logger.Scene("analytics").Error(err, "访问统计查询失败")
	return dashboardenums.MsgInternalError
}
