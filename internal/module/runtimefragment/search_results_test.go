package runtimefragment

// search_results_test.go — 站内搜索结果片段单测（BIZ-2）。
//
// 三条硬约束各配一个用例：
//
//	1. 未发布内容不外泄 —— 内容命中没有「已上线路径」时整条丢弃（contents 表没有状态列，
//	   「已发布」在片段这一层只能等同于「有线上页面」）；
//	2. 链接必须真实可用 —— 路径一律来自发布面，拿不到就只输出标题（绝不拼 slug 约定）；
//	3. 降级不 500 —— 空关键词 / 端口未接入 / 无结果都给人话。

import (
	"context"
	"errors"
	"strings"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
)

// stubContentSearch 内容检索桩。
type stubContentSearch struct {
	hits []*contentcontract.ArticleSearchHit
	err  error
}

func (s *stubContentSearch) SearchArticles(_ context.Context, _ string, _ int) ([]*contentcontract.ArticleSearchHit, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.hits, nil
}

// stubProductSearch 商品检索桩。
type stubProductSearch struct {
	hits []*productcontract.ProductSearchHit
	err  error
}

func (s *stubProductSearch) SearchPublishedProducts(_ context.Context, _, _ string, _ int) ([]*productcontract.ProductSearchHit, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.hits, nil
}

// stubPublishedLocator 「实体 → 已上线路径」桩：只返回表里有的键。
type stubPublishedLocator struct {
	paths map[string]string
	err   error
}

func (s *stubPublishedLocator) PublishedEntityPaths(_ context.Context, _, _, _ string, ids []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]string{}
	for _, id := range ids {
		if path, ok := s.paths[id]; ok {
			out[id] = path
		}
	}
	return out, nil
}

// resetSearchProviders 清空三条端口（测试之间不互相污染）。
func resetSearchProviders(t *testing.T) {
	t.Helper()
	deps.ContentSearchProvider = nil
	deps.ProductSearchProvider = nil
	deps.PublishedEntityLocator = nil
	t.Cleanup(func() {
		deps.ContentSearchProvider = nil
		deps.ProductSearchProvider = nil
		deps.PublishedEntityLocator = nil
	})
}

// searchRequest 一次搜索请求（工程 id 由构建期烘进 URL）。
func searchRequest(q string) *Request {
	return &Request{
		Type: "searchResults", Lang: "zh-CN",
		T:      func(_, fallback string) string { return fallback },
		Params: map[string]string{"q": q, "projectId": "p1"},
	}
}

// TestSearchResultsEmptyQuery 空关键词：提示而不是报错（搜索框还没输入就会触发请求）。
func TestSearchResultsEmptyQuery(t *testing.T) {
	resetSearchProviders(t)
	deps.ContentSearchProvider = &stubContentSearch{}
	out, err := renderSearchResults(context.Background(), searchRequest("   "))
	if err != nil {
		t.Fatalf("空关键词不该报错: %v", err)
	}
	if !strings.Contains(out, "请输入搜索关键词") {
		t.Fatalf("缺少空关键词提示:\n%s", out)
	}
}

// TestSearchResultsDegraded 两条检索端口都没接入：给降级文案，不是 500。
func TestSearchResultsDegraded(t *testing.T) {
	resetSearchProviders(t)
	out, err := renderSearchResults(context.Background(), searchRequest("鞋"))
	if err != nil {
		t.Fatalf("端口未接入不该报错: %v", err)
	}
	if !strings.Contains(out, "搜索功能暂未接入") {
		t.Fatalf("缺少降级文案:\n%s", out)
	}
}

// TestSearchResultsDropsUnpublishedContent 内容命中但没有已上线路径 → 整条丢弃。
func TestSearchResultsDropsUnpublishedContent(t *testing.T) {
	resetSearchProviders(t)
	deps.ContentSearchProvider = &stubContentSearch{hits: []*contentcontract.ArticleSearchHit{
		{ID: "a1", Slug: "draft", Title: "还没发布的草稿", Excerpt: "内部预览用"},
	}}
	deps.PublishedEntityLocator = &stubPublishedLocator{paths: map[string]string{}}

	out, err := renderSearchResults(context.Background(), searchRequest("草稿"))
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.Contains(out, "还没发布的草稿") {
		t.Fatalf("未发布内容的标题不得出现在搜索结果里:\n%s", out)
	}
	if !strings.Contains(out, "没有找到与") {
		t.Fatalf("应给出「没有找到」结论:\n%s", out)
	}
}

// TestSearchResultsContentWithPath 有已上线路径 → 输出可点链接，路径逐字来自发布面。
func TestSearchResultsContentWithPath(t *testing.T) {
	resetSearchProviders(t)
	deps.ContentSearchProvider = &stubContentSearch{hits: []*contentcontract.ArticleSearchHit{
		{ID: "a1", Slug: "hello", Title: "你好，世界", Excerpt: "第一篇"},
	}}
	deps.PublishedEntityLocator = &stubPublishedLocator{paths: map[string]string{"a1": "/blog/hello/"}}

	out, err := renderSearchResults(context.Background(), searchRequest("hello"))
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "href=\"/blog/hello/\"") {
		t.Fatalf("缺少结果链接:\n%s", out)
	}
	if !strings.Contains(out, "你好，世界") {
		t.Fatalf("缺少结果标题:\n%s", out)
	}
}

// TestSearchResultsProductWithoutPath 商品已上架但还没有详情页 → 输出条目但不给链接。
func TestSearchResultsProductWithoutPath(t *testing.T) {
	resetSearchProviders(t)
	deps.ProductSearchProvider = &stubProductSearch{hits: []*productcontract.ProductSearchHit{
		{ID: "11111111-1111-1111-1111-111111111111", Name: "帆布鞋", Subtitle: "轻便透气"},
	}}
	deps.PublishedEntityLocator = &stubPublishedLocator{paths: map[string]string{}}

	out, err := renderSearchResults(context.Background(), searchRequest("鞋"))
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "帆布鞋") {
		t.Fatalf("上架商品应出现在结果里:\n%s", out)
	}
	if strings.Contains(out, "href=") {
		t.Fatalf("没有线上路径时不该输出任何链接（死链）:\n%s", out)
	}
}

// TestSearchResultsEscapesQuery 关键词是用户输入：回显时必须被模板转义。
func TestSearchResultsEscapesQuery(t *testing.T) {
	resetSearchProviders(t)
	deps.ContentSearchProvider = &stubContentSearch{}
	deps.PublishedEntityLocator = &stubPublishedLocator{}

	out, err := renderSearchResults(context.Background(), searchRequest("<script>alert(1)</script>"))
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.Contains(out, "<script>") {
		t.Fatalf("关键词未转义（XSS）:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("转义后的关键词应出现在提示里:\n%s", out)
	}
}

// TestSearchResultsPassesLangToLocator 片段 lang 参数透传给路径解析端口。
func TestSearchResultsPassesLangToLocator(t *testing.T) {
	resetSearchProviders(t)
	langSeen := ""
	loc := &stubPublishedLocator{paths: map[string]string{"a1": "/en/blog/hello"}}
	locHook := &langCapturingLocator{inner: loc, lang: &langSeen}
	deps.ContentSearchProvider = &stubContentSearch{hits: []*contentcontract.ArticleSearchHit{
		{ID: "a1", Slug: "hello", Title: "Hello"},
	}}
	deps.PublishedEntityLocator = locHook

	req := &Request{
		Type: "searchResults", Lang: "en-US",
		T: func(_, fallback string) string { return fallback },
		Params: map[string]string{
			"q": "hello", "projectId": "p1", "lang": "en-US",
		},
	}
	out, err := renderSearchResults(context.Background(), req)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if langSeen != "en-US" {
		t.Fatalf("locator 应收到 lang=en-US，实际 %q", langSeen)
	}
	if !strings.Contains(out, `href="/en/blog/hello"`) {
		t.Fatalf("缺少 en 路径链接:\n%s", out)
	}
}

type langCapturingLocator struct {
	inner presentationcontract.PublishedEntityLocator
	lang  *string
}

func (l *langCapturingLocator) PublishedEntityPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (map[string]string, error) {
	*l.lang = lang
	return l.inner.PublishedEntityPaths(ctx, projectID, entityType, lang, entityIDs)
}

// TestSearchResultsPortError 检索端口报错要上抛（端点转 500）。
func TestSearchResultsPortError(t *testing.T) {
	resetSearchProviders(t)
	deps.ContentSearchProvider = &stubContentSearch{err: errors.New("数据库不可用")}
	if _, err := renderSearchResults(context.Background(), searchRequest("x")); err == nil {
		t.Fatalf("端口报错应上抛")
	}
}

// TestSearchResultLimit 条数解析：缺省值、上限封顶、非法回落。
func TestSearchResultLimit(t *testing.T) {
	if got := searchResultLimit(""); got != searchDefaultLimit {
		t.Errorf("缺省应为 %d，实际 %d", searchDefaultLimit, got)
	}
	if got := searchResultLimit("3"); got != 3 {
		t.Errorf("显式值应为 3，实际 %d", got)
	}
	if got := searchResultLimit("9999"); got != searchMaxLimit {
		t.Errorf("超限应封顶到 %d，实际 %d", searchMaxLimit, got)
	}
	if got := searchResultLimit("abc"); got != searchDefaultLimit {
		t.Errorf("非法值应回落缺省，实际 %d", got)
	}
}
