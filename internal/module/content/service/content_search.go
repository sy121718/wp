// content_search.go — 内容检索（实现 contract.SearchPort，BIZ-2 站内搜索）。
//
// 只读路径：片段端点（anonymous GET）→ SearchPort → model.SearchArticles → contents 表。
// 本文件不判定「已发布」—— contents 表没有状态列，内容的线上可用性由「有没有已上线的
// 详情页产物」决定（presentation 实例的 active 指针），那个判定在搜索片段那一层与
// 发布面的路径解析一起做（见 runtimefragment/search_results.go）。
package contentservice

import (
	"context"
	"encoding/json"
	"strings"

	contentcontract "go_wp/internal/module/content/contract"
)

// 编译期断言：本 service 提供访问面检索需要的只读能力。
var _ contentcontract.SearchPort = (*Service)(nil)

// SearchArticles 按关键词检索文章（实现 contentcontract.SearchPort）。
//
// 取词用**原文**而不是译文：检索命中的是内容本身的字，拿译文去命中会让
// 「搜中文标题、原文是英文」这类情况永远搜不到（多语言检索属后续票的范围）。
func (s *Service) SearchArticles(ctx context.Context, keyword string, limit int) (hits []*contentcontract.ArticleSearchHit, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	rows, err := s.m.SearchArticles(ctx, contentcontract.EntityTypeArticle, keyword, limit)
	if err != nil {
		return nil, err
	}
	hits = make([]*contentcontract.ArticleSearchHit, 0, len(rows))
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.ID) == "" {
			continue
		}
		title, excerpt := articleSearchText(row.Data)
		hits = append(hits, &contentcontract.ArticleSearchHit{
			ID: row.ID, Slug: row.Slug, Title: title, Excerpt: excerpt,
		})
	}
	return hits, nil
}

// articleSearchText 从 contents.data（JSONB）取标题与摘要。
//
// 解析失败或字段缺失时返回空串而不是报错：结果条目少一行标题，比整页搜索 500 好得多；
// 而 data 的形状在保存期已由字段白名单校验过，这里的容错是给历史数据兜底。
func articleSearchText(raw json.RawMessage) (title, excerpt string) {
	if len(raw) == 0 {
		return "", ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", ""
	}
	title, _ = fields["title"].(string)
	excerpt, _ = fields["excerpt"].(string)
	return strings.TrimSpace(title), strings.TrimSpace(excerpt)
}
