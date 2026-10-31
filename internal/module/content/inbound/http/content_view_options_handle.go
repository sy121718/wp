package contenthttp

import "go_wp/pkg/pagination"

type OptionResp struct {
	ID   string
	Name string
}

type TemplateOptionResp struct {
	ID      string
	Name    string
	Version int64
}

type StalePageRowResp struct {
	ID          string
	Path        string
	ProjectID   string
	ProjectName string

	Published bool

	RebuildFailure any
}

type StaleImpactResp struct {
	Available bool
	Pages     []StalePageRowResp
	Total     int
	Truncated bool
	Limit     int
	Hint      any
}

type ArticleFormResp struct {
	ID        string
	Slug      string
	Title     string
	Body      string
	Revision  int64
	UpdatedAt string
}

type ArticleNewPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`
	Form  *ArticleFormResp
}

type ArticleSeoDrawerResp struct {
	Score any
	IsNew bool
}

type ArticleListRowResp struct {
	ID        string
	Title     string
	Slug      string
	Excerpt   string
	Revision  int64
	UpdatedAt any
	Published bool
	URLPath   string
	PublicURL string
	EditURL   string

	StateKey      string
	StateFallback string
}

type ArticleListPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`
	Rows  []ArticleListRowResp
	Total int
	Empty bool
	Err   any

	BlogBase string
	Keyword  string

	ClearFilterURL string
	Limit          int

	FilterQuery string

	StaleImpact *StaleImpactResp

	Pagination pagination.Page
}
