package analyticsdto

// analytics_page_resp.go — 访问统计页的渲染视图（页面 GET 的回执形状）。
//
// 与 analytics_resp.go 的分工：那一份是**接口契约**（JSON API 的聚合响应）；
// 这一份是**页面视图**（模板直接读的字段）。分开的理由与 product / masterdata /
// contenttemplate 一致：读一个类型不必先判断它是进还是出。
//
// **导出字段名就是模板契约**：internal/templates/admin/analytics/analytics.html 里的
// `.Field` 必须在这里有同名导出字段。这个页面原先用 gin.H 拼了 20+ 个键，
// 拼错一个的表现是「HTTP 200 + 之后整块 HTML 消失」（Jet 缺键中断），
// 而编译器对 map 键一无所知 —— 换成结构体后改名即编译错误。
//
// 分页仍是「现成数据对象」：adminui.AttachPaginationView 往本视图的 Pagination
// 字段上挂分页事实，模板侧与 gin.H 时代完全一样。

import (
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

// AnalyticsPageResp 访问统计整页的渲染视图。
type AnalyticsPageResp struct {
	// Title / Menu 是 layout 的小写壳键（{{.title}} / {{.menu}}）的值。
	Title string
	Menu  string

	// 工程上下文与筛选条件。
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	FilterFrom      string
	FilterTo        string
	RangeFrom       string
	RangeTo         string

	// 概览数字。
	Total    int64
	Visitors int64

	// 维度榜（按天 / 按路径 / 来源 / UA / 语言）。
	Daily     []DailyCount
	Paths     []PathCount
	PathTotal int64
	Referrers []RankCount
	UAClasses []RankCount
	Langs     []RankCount
	RankLimit int

	// ViewMode 初始选中的维度视图（tabs）：模板据此决定 5 个标签里哪个 aria-selected、
	// 哪个面板不加 hidden。模板对缺键有兜底（回落 daily），所以别的渲染路径漏传不会断页。
	ViewMode string

	// BreakdownUnavailable 只有「总数有数、维度榜全空」这一种情况给解释。
	BreakdownUnavailable bool

	// Err 取数失败时的归口提示（空串 = 无错误）。
	Err string
	// LoadFailed 本次请求的工程列表没读到（**不是**「还没有工程」）。
	// 模板据此把空态换成「没读出来」—— 显示「还没有站点工程」会让运营去建工程，
	// 而真正的问题是这一次没读出来。
	LoadFailed bool

	// Pagination 分页事实（由 adminui.AttachPaginationView 反射挂上）。
	//
	// 键名必须是 view.PaginationKey（"Pagination"）—— 模板的
	// admin/partials/pagination.html 按这个键读。字段显式声明在这里，
	// 而不是让渲染层"偷偷长一个出来"：模板契约要看得见。
	Pagination pagination.Page
}

// SEOPageResp SEO 控制台的渲染视图（/admin/seo）。
//
// 与 AnalyticsPageResp 分开：两个页面的键集不同，合成一个类型会让"这一屏要哪些键"
// 变得不可读，而 Jet 对缺键的处理是**整页中断** —— 键集边界必须显式。
type SEOPageResp struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Paths           []PathCount
	HasPaths        bool
	// AnalyticsError 热门路径读不出来（页面提示条）。
	AnalyticsError bool
	// ProjectError 站点列表读不出来（**不是**「还没有站点」）。
	ProjectError bool
}
