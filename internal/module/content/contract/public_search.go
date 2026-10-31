package contentcontract

import (
	"context"
	contentdto "go_wp/internal/module/content/dto"
)

type PublicSearchData struct {
	Articles []PublicArticleHit `json:"articles"`
	Products []PublicProductHit `json:"products"`
}
type PublicArticleHit struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
	URL     string `json:"url"`
}
type PublicProductHit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Subtitle string `json:"subtitle"`
	URL      string `json:"url"`
}
type PublicSearchReader interface {
	PublicSearch(context.Context, *contentdto.PublicSearchReq) (*PublicSearchData, error)
}
