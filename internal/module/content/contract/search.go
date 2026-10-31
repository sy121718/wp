package contentcontract

import "context"

const EntityTypeArticle = "article"

type ArticleSearchHit struct {
	ID      string
	Slug    string
	Title   string
	Excerpt string
}

type SearchPort interface {
	SearchArticles(ctx context.Context, keyword string, limit int) (hits []*ArticleSearchHit, err error)
}
