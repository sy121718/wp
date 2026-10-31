package analyticsdto

const (
	SourceDetail = "detail"

	SourceSummary = "summary"
)

type DailyCount struct {
	Day string `json:"day"`

	Views int64 `json:"views"`

	Visitors int64 `json:"visitors"`
}

type PathCount struct {
	Path     string `json:"path"`
	Views    int64  `json:"views"`
	Visitors int64  `json:"visitors"`
}

type RankCount struct {
	Value    string `json:"value"`
	Views    int64  `json:"views"`
	Visitors int64  `json:"visitors"`
}

type SummaryResp struct {
	ProjectID string `json:"projectId"`

	From string `json:"from"`
	To   string `json:"to"`

	Total int64 `json:"total"`

	Visitors int64 `json:"visitors"`

	Daily []DailyCount `json:"daily"`

	Paths []PathCount `json:"paths"`

	PathTotal int64 `json:"pathTotal"`

	PathPage  int `json:"pathPage"`
	PathLimit int `json:"pathLimit"`

	Referrers []RankCount `json:"referrers"`
	UAClasses []RankCount `json:"uaClasses"`
	Langs     []RankCount `json:"langs"`

	RankLimit int `json:"rankLimit"`

	BreakdownSource string `json:"breakdownSource"`

	Source string `json:"source"`

	PathNextAfterViews int64  `json:"pathNextAfterViews"`
	PathNextAfter      string `json:"pathNextAfter"`
}
