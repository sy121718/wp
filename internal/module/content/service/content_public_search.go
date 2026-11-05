package contentservice

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/pkg/seo"
)

type PublicSearchService struct {
	articles     contentcontract.SearchPort
	products     productcontract.SearchPort
	publications presentationcontract.PublishedEntityIndex
}

func NewPublicSearchService(articles contentcontract.SearchPort, products productcontract.SearchPort, publications presentationcontract.PublishedEntityIndex) *PublicSearchService {
	return &PublicSearchService{articles: articles, products: products, publications: publications}
}
func (s *PublicSearchService) PublicSearch(ctx context.Context, req *contentdto.PublicSearchReq) (*contentcontract.PublicSearchData, error) {
	if req == nil || uuid.Validate(req.ProjectID) != nil || utf8.RuneCountInString(req.Query) > 64 || req.Limit < 0 || req.Limit > 20 {
		return nil, contentenums.Err(contentenums.ErrInvalidParam)
	}
	out := &contentcontract.PublicSearchData{Articles: []contentcontract.PublicArticleHit{}, Products: []contentcontract.PublicProductHit{}}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return out, nil
	}
	if s.articles == nil || s.products == nil || s.publications == nil {
		return nil, fmt.Errorf("公开搜索端口未装配")
	}
	limit := req.Limit
	if limit == 0 {
		limit = 8
	}
	articlePaths, err := s.publications.PublishedEntityIndex(ctx, req.ProjectID, contentcontract.EntityTypeArticle, req.Lang)
	if err != nil {
		return nil, err
	}
	articles, err := s.articles.SearchArticles(ctx, query, 20)
	if err != nil {
		return nil, err
	}
	for _, hit := range articles {
		if hit == nil || strings.TrimSpace(articlePaths[hit.ID]) == "" {
			continue
		}
		out.Articles = append(out.Articles, contentcontract.PublicArticleHit{ID: hit.ID, Title: hit.Title, Excerpt: hit.Excerpt, URL: seo.AbsoluteSiteURL(articlePaths[hit.ID])})
		if len(out.Articles) >= limit {
			break
		}
	}
	productPaths, err := s.publications.PublishedEntityIndex(ctx, req.ProjectID, productcontract.EntityTypeProduct, req.Lang)
	if err != nil {
		return nil, err
	}
	products, err := s.products.SearchPublishedProducts(ctx, req.ProjectID, query, 20)
	if err != nil {
		return nil, err
	}
	for _, hit := range products {
		if hit == nil || strings.TrimSpace(productPaths[hit.ID]) == "" {
			continue
		}
		out.Products = append(out.Products, contentcontract.PublicProductHit{ID: hit.ID, Name: hit.Name, Subtitle: hit.Subtitle, URL: seo.AbsoluteSiteURL(productPaths[hit.ID])})
		if len(out.Products) >= limit {
			break
		}
	}
	return out, nil
}
