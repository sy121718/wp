package productcontract

import "context"

type ProductSearchHit struct {
	ID           string
	Name         string
	Subtitle     string
	Slug         string
	DefaultImage string
}

type SearchPort interface {
	SearchPublishedProducts(ctx context.Context, projectID, keyword string, limit int) (hits []*ProductSearchHit, err error)
}
