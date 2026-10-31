package contenthttp

type SeoScoreFragmentView struct {
	Score any
}

type ArticleImportFragmentView struct {
	Preview any

	Err any
}

type StalePagesDrawerResp struct {
	Rows  []StalePageRowResp
	Total int

	Truncated bool

	Limit int

	MultiProject bool
}
