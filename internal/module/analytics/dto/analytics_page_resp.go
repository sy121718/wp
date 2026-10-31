package analyticsdto

import (
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

type AnalyticsPageResp struct {
	Title string
	Menu  string

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

	BreakdownUnavailable bool

	Err string

	LoadFailed bool

	Pagination pagination.Page
}

type SEOPageResp struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Paths           []PathCount
	HasPaths        bool

	AnalyticsError bool

	ProjectError bool
}
