package pubservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/i18n"
)

func feedTestArticleHTML(title, desc string) string {
	return "<html><head><title>" + title + "</title>" +
		"<meta name=\"description\" content=\"" + desc + "\">" +
		"<link rel=\"canonical\" href=\"/x\">" +
		"<script type=\"application/ld+json\">{\"@context\":\"https://schema.org\",\"@type\":\"Article\",\"headline\":\"" +
		title + "\"}</script></head><body></body></html>"
}

func feedTestProductHTML(title string) string {
	return "<html><head><title>" + title + "</title>" +
		"<script type=\"application/ld+json\">{\"@context\":\"https://schema.org\",\"@type\":\"Product\",\"name\":\"" +
		title + "\"}</script></head><body></body></html>"
}

func writeActiveArtifact(t *testing.T, dir, path, body string) {
	t.Helper()
	rel := strings.Trim(strings.TrimSpace(path), "/")
	if rel == "" {
		rel = "index"
	}
	target := filepath.Join(dir, filepath.FromSlash(rel), "index.html")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("创建产物目录失败: %v", err)
	}
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("写入产物失败: %v", err)
	}
}

func feedTestRoute(path string, presentation bool, updated time.Time) pubmodel.RouteEntity {
	rt := pubmodel.RouteEntity{
		ProjectID: "p-1", Path: path, RouteKind: pubmodel.RouteActive, UpdatedAt: updated,
	}
	if presentation {
		id := "11111111-1111-1111-1111-111111111111"
		rt.PresentationID = &id
	}
	return rt
}

func TestFeedItemsCollectsArticleInstancesOnly(t *testing.T) {

	dir := t.TempDir()
	old := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	writeActiveArtifact(t, dir, "/blog/old", feedTestArticleHTML("旧文章", "旧摘要"))
	writeActiveArtifact(t, dir, "/blog/new", feedTestArticleHTML("新文章", "新摘要"))
	writeActiveArtifact(t, dir, "/products/p1", feedTestProductHTML("某商品"))
	writeActiveArtifact(t, dir, "/about", feedTestArticleHTML("关于我们", "手工页面"))

	routes := []pubmodel.RouteEntity{
		feedTestRoute("/blog/old", true, old),
		feedTestRoute("/blog/new", true, fresh),
		feedTestRoute("/products/p1", true, fresh),
		feedTestRoute("/about", false, fresh),
	}
	items := feedItems(dir, "https://e.com", routes, nil, "", string(i18n.SiteLangURLModeOff))
	if len(items) != 2 {
		t.Fatalf("应只收 2 篇文章，实际 %d 条: %+v", len(items), items)
	}
	if items[0].Link != "https://e.com/blog/new" || items[1].Link != "https://e.com/blog/old" {
		t.Fatalf("条目应按最近激活时刻倒序，实际 %+v", items)
	}
	if items[0].Title != "新文章" || items[0].Description != "新摘要" {
		t.Fatalf("标题/摘要应取自产物 <head>，实际 %+v", items[0])
	}
	if !items[0].PubDate.Equal(fresh) {
		t.Fatalf("pubDate 应取路由行的 update_time，实际 %v", items[0].PubDate)
	}
}

func TestFeedItemsSkipsMissingArtifact(t *testing.T) {

	dir := t.TempDir()
	writeActiveArtifact(t, dir, "/blog/here", feedTestArticleHTML("在这儿", "d"))
	routes := []pubmodel.RouteEntity{
		feedTestRoute("/blog/here", true, time.Now().UTC()),
		feedTestRoute("/blog/gone", true, time.Now().UTC()),
	}
	items := feedItems(dir, "https://e.com", routes, nil, "", string(i18n.SiteLangURLModeOff))
	if len(items) != 1 || items[0].Link != "https://e.com/blog/here" {
		t.Fatalf("产物缺失的路径应被跳过，实际 %+v", items)
	}
}

func TestFeedItemsMultiLangKeepsDefaultOnly(t *testing.T) {

	dir := t.TempDir()
	writeActiveArtifact(t, dir, "/blog/hello", feedTestArticleHTML("你好", "默认语言"))
	writeActiveArtifact(t, dir, "/en/blog/hello", feedTestArticleHTML("Hello", "english"))

	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	routes := []pubmodel.RouteEntity{
		feedTestRoute("/blog/hello", true, now),
		feedTestRoute("/en/blog/hello", true, now),
	}
	items := feedItems(dir, "https://e.com", routes, []string{"zh-CN", "en-US"}, "zh-CN", string(i18n.SiteLangURLModeDefaultPlain))
	if len(items) != 1 {
		t.Fatalf("多语言站点应只收默认语言条目，实际 %+v", items)
	}
	if items[0].Link != "https://e.com/blog/hello" {
		t.Fatalf("应收默认语言路径 /blog/hello，实际 %q", items[0].Link)
	}
}

func TestFeedItemsKeepsAllWhenDefaultLangUnknown(t *testing.T) {

	dir := t.TempDir()
	writeActiveArtifact(t, dir, "/blog/hello", feedTestArticleHTML("你好", "d"))
	writeActiveArtifact(t, dir, "/en/blog/hello", feedTestArticleHTML("Hello", "d"))
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	routes := []pubmodel.RouteEntity{
		feedTestRoute("/blog/hello", true, now),
		feedTestRoute("/en/blog/hello", true, now),
	}
	items := feedItems(dir, "https://e.com", routes, []string{"zh-CN", "en-US"}, "", string(i18n.SiteLangURLModeDefaultPlain))
	if len(items) != 2 {
		t.Fatalf("默认语言未知时不应过滤，实际 %+v", items)
	}
}

func TestFeedChannelFromHomeArtifact(t *testing.T) {

	dir := t.TempDir()
	writeActiveArtifact(t, dir, "/", "<html><head><title>示例站</title>"+
		"<meta name=\"description\" content=\"站点描述\"></head><body></body></html>")

	ch := feedChannel(dir, "https://e.com", nil, nil, "", string(i18n.SiteLangURLModeOff))
	if ch.Title != "示例站" || ch.Description != "站点描述" {
		t.Fatalf("channel 应取首页产物的 title/description，实际 %+v", ch)
	}
	if ch.Link != "https://e.com/" {
		t.Fatalf("channel link 应为站点根，实际 %q", ch.Link)
	}

	empty := t.TempDir()
	fallback := feedChannel(empty, "https://e.com", nil, nil, "", string(i18n.SiteLangURLModeOff))
	if fallback.Title != "e.com" {
		t.Fatalf("取不到首页产物时应回退 host，实际 %q", fallback.Title)
	}
}

func TestActiveHTMLFileFollowsSymlinkedDir(t *testing.T) {
	active := t.TempDir()
	artifacts := t.TempDir()

	for _, c := range []struct{ seg, body string }{
		{"about", feedTestArticleHTML("关于", "d")},
		{"index", feedTestArticleHTML("首页", "d")},
	} {
		hashDir := filepath.Join(artifacts, c.seg)
		if err := os.MkdirAll(hashDir, 0o755); err != nil {
			t.Fatalf("创建产物目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(hashDir, "index.html"), []byte(c.body), 0o644); err != nil {
			t.Fatalf("写入产物失败: %v", err)
		}
		if err := os.Symlink(hashDir, filepath.Join(active, c.seg)); err != nil {
			t.Fatalf("创建激活链接失败: %v", err)
		}
	}

	for _, tc := range []struct{ path, wantTitle string }{
		{"/about", "关于"},
		{"/", "首页"},
		{"/index", "首页"},
	} {
		file, ok := activeHTMLFile(active, tc.path)
		if !ok {
			t.Fatalf("路径 %s 的产物未找到", tc.path)
		}
		doc, ok := readFeedDoc(file)
		if !ok || doc.Title != tc.wantTitle {
			t.Fatalf("路径 %s 应解析出标题 %q，实际 %+v (ok=%v)", tc.path, tc.wantTitle, doc, ok)
		}
	}

	if _, ok := activeHTMLFile(active, "/nope"); ok {
		t.Fatal("不存在的路径不应返回文件")
	}
}

func TestParseFeedDocTypes(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   bool
	}{
		{"字符串", "{\"@type\":\"Article\"}", true},
		{"数组", "{\"@type\":[\"WebPage\",\"BlogPosting\"]}", true},
		{"graph", "{\"@graph\":[{\"@type\":\"NewsArticle\"}]}", true},
		{"带前缀", "{\"@type\":\"https://schema.org/Article\"}", true},
		{"商品", "{\"@type\":\"Product\"}", false},
		{"坏 JSON", "{not json", false},
	}
	for _, c := range cases {
		doc, err := parseFeedDoc(strings.NewReader(
			"<html><head><script type=\"application/ld+json\">" + c.script +
				"</script></head><body></body></html>"))
		if err != nil {
			t.Fatalf("%s: 解析失败: %v", c.name, err)
		}
		if got := doc.isArticle(); got != c.want {
			t.Errorf("%s: isArticle 应得 %v，实际 %v（types=%v）", c.name, c.want, got, doc.Types)
		}
	}
}
