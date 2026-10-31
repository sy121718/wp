package contentdto

type ArticleStaleImpactResp struct {
	Available bool
	Pages     any
	Total     int
	Truncated bool
	Limit     int
	Hint      any
}
