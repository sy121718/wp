// product_search.go — 商品检索（实现 contract.SearchPort，BIZ-2 站内搜索）。
//
// 只读路径：片段端点（anonymous GET）→ SearchPort → model.SearchPublished → products 表。
// 「已上架」直接落在 SQL 条件里，不在本层二次过滤 —— 少一层过滤就少一处
// 「换了个入口就绕过了」的可能。
package productservice

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
)

// 编译期断言：本 service 提供访问面检索需要的只读能力。
var _ productcontract.SearchPort = (*Service)(nil)

// SearchPublishedProducts 按关键词检索已上架商品（实现 productcontract.SearchPort）。
func (s *Service) SearchPublishedProducts(ctx context.Context, projectID, keyword string, limit int) (hits []*productcontract.ProductSearchHit, err error) {
	projectID = strings.TrimSpace(projectID)
	keyword = strings.TrimSpace(keyword)
	if projectID == "" || keyword == "" {
		return nil, nil
	}
	rows, err := s.m.SearchPublished(ctx, projectID, keyword, limit)
	if err != nil {
		return nil, err
	}
	hits = make([]*productcontract.ProductSearchHit, 0, len(rows))
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.ID) == "" {
			continue
		}
		hits = append(hits, &productcontract.ProductSearchHit{
			ID: row.ID, Name: row.Name, Subtitle: row.Subtitle,
			Slug: row.Slug, DefaultImage: row.DefaultImage,
		})
	}
	return hits, nil
}
