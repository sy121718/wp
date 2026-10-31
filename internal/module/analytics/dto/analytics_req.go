package analyticsdto

type CollectReq struct {
	ProjectID string

	Path string

	Lang string

	Referrer string

	Session string

	Visitor string

	IP string

	UserAgent string
}

type SummaryReq struct {
	ProjectID string `form:"projectId" json:"projectId"`

	From string `form:"from" json:"from"`

	To string `form:"to" json:"to"`

	PathPage int `form:"pathPage" json:"pathPage"`

	PathLimit int `form:"pathLimit" json:"pathLimit"`

	PathAfterViews int64  `form:"pathAfterViews" json:"pathAfterViews"`
	PathAfter      string `form:"pathAfter" json:"pathAfter"`

	RankLimit int `form:"rankLimit" json:"rankLimit"`

	Granularity string `form:"granularity" json:"granularity"`
}
