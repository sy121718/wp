package analyticsdto

import (
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

// AnalyticsPageResp 只装页面渲染需要的事实：工程列表、筛选值、聚合结果、分页事实。
//
// 标题不在这里 —— 页面标题是界面文案，归模板的 pageTitle() / topbarTitle()（用全局
// t 取词）。失败也不在这里 —— 取数失败统一走 JSON 早退（真实状态码），页面不需要
// 「失败态」字段，更不渲染失败页。
type AnalyticsPageResp struct {
	Menu string

	Projects        []projectcontract.ProjectResp
	SelectedProject string
	FilterFrom      string
	FilterTo        string
	RangeFrom       string
	RangeTo         string

	Total    int64
	Visitors int64

	Daily     []DailyCount
	Paths     []PathCount
	PathTotal int64
	Referrers []RankCount
	UAClasses []RankCount
	Langs     []RankCount
	RankLimit int

	ViewMode string

	// BreakdownUnavailable 是数据完整性事实（明细过保留期被清理），不是读取失败：
	// 汇总数字仍有效，只需要在维度榜空态处解释原因。
	BreakdownUnavailable bool

	Pagination pagination.Page
}

// Paging 只交三个事实（总数 / 当前页 / 每页条数）。
// 窗口、Base 与每页链接全部归模板：partial 用 pageBase(req_path, "page", "limit")
// 从当前请求 URI 推 Base，控制器既不拼 URL 也不给 Base。
func (v *AnalyticsPageResp) Paging() pagination.Page {
	return v.Pagination
}

type SEOPageResp struct {
	Menu string

	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Paths           []PathCount
	HasPaths        bool
}
