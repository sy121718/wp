package pubservice

// publication_feed_xml_test.go — feed 落到激活目录之后的端到端校验（审计 SEO-012）。
//
// 与 publication_feed_test.go 的分工：那个文件测条目收集的取舍，本文件测
// SEO-012 verification 原样要求的三条 ——
//
//  1. feed 能被校验器解析（标准库 XML 解析 + 必需元素非空）；
//  2. 条目链接可访问（每个条目的 URL 在激活目录里真有产物文件）；
//  3. 关闭开关后不再生成（既有 feed 被删除，且不需要数据库即可判定）。

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/seo"
	"go_wp/pkg/i18n"
)

// feedXML 校验用的最小 RSS 2.0 解析结构（只取 verification 关心的元素）。
type feedXML struct {
	Version string "xml:\"version,attr\""
	Channel struct {
		Title       string "xml:\"title\""
		Link        string "xml:\"link\""
		Description string "xml:\"description\""
		Items       []struct {
			Title   string "xml:\"title\""
			Link    string "xml:\"link\""
			PubDate string "xml:\"pubDate\""
			GUID    struct {
				IsPermaLink string "xml:\"isPermaLink,attr\""
				Value       string "xml:\",chardata\""
			} "xml:\"guid\""
		} "xml:\"item\""
	} "xml:\"channel\""
}

// TestFeedXMLParsesAndItemLinksResolve 写出的 feed 可解析、必需元素齐全、条目链接可达。
func TestFeedXMLParsesAndItemLinksResolve(t *testing.T) {

	dir := t.TempDir()
	writeActiveArtifact(t, dir, "/", "<html><head><title>示例站</title>"+
		"<meta name=\"description\" content=\"站点描述\"></head><body></body></html>")
	writeActiveArtifact(t, dir, "/blog/hello", feedTestArticleHTML("你好 & 世界", "含 <b>标记</b> 的摘要"))

	routes := []pubmodel.RouteEntity{
		feedTestRoute("/blog/hello", true, time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)),
	}
	items := feedItems(dir, "https://e.com", routes, nil, "", string(i18n.SiteLangURLModeOff))
	ch := feedChannel(dir, "https://e.com", nil, nil, "", string(i18n.SiteLangURLModeOff))
	if err := seo.WriteFeed(dir, ch, items, seo.FeedLimit()); err != nil {
		t.Fatalf("写 feed 失败: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, seo.FeedFileName))
	if err != nil {
		t.Fatalf("读取 feed 失败: %v", err)
	}
	var parsed feedXML
	if err := xml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("feed 不能被 XML 解析器解析: %v\n%s", err, string(raw))
	}
	if parsed.Version != "2.0" {
		t.Fatalf("rss version 应为 2.0，实际 %q", parsed.Version)
	}
	if parsed.Channel.Title != "示例站" || parsed.Channel.Description != "站点描述" {
		t.Fatalf("channel 必需元素不对: %+v", parsed.Channel)
	}
	if parsed.Channel.Link != "https://e.com/" {
		t.Fatalf("channel link 应为站点根，实际 %q", parsed.Channel.Link)
	}
	if len(parsed.Channel.Items) != 1 {
		t.Fatalf("应有 1 条条目，实际 %d", len(parsed.Channel.Items))
	}
	item := parsed.Channel.Items[0]
	if item.Title != "你好 & 世界" || item.PubDate != "Mon, 14 Sep 2026 10:00:00 +0000" {
		t.Fatalf("条目内容不对: %+v", item)
	}
	if item.GUID.Value != item.Link || item.GUID.IsPermaLink != "true" {
		t.Fatalf("guid 应为条目链接本身: %+v", item.GUID)
	}

	// 条目链接可访问：URL 去掉站点根之后必须在激活目录里找得到产物。
	for _, it := range parsed.Channel.Items {
		if !strings.HasPrefix(it.Link, "https://e.com/") {
			t.Fatalf("条目链接不在站点根之下: %q", it.Link)
		}
		p := strings.TrimPrefix(it.Link, "https://e.com")
		if _, ok := activeHTMLFile(dir, p); !ok {
			t.Fatalf("条目链接 %s 在激活目录里没有对应产物（死链）", it.Link)
		}
	}
}

// TestRefreshFeedDisabledRemovesFeed 关闭开关后不再生成：既有 feed 被删除。
//
// Service 零值即可走这条分支（开关关闭时直接删除、不查库），因此这条 verification
// 不需要数据库就能跑 —— 也正是它该被钉成单测的理由。
func TestRefreshFeedDisabledRemovesFeed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, seo.FeedFileName)
	if err := os.WriteFile(path, []byte("<?xml version=\"1.0\"?><rss/>"), 0o644); err != nil {
		t.Fatalf("准备既有 feed 失败: %v", err)
	}

	t.Setenv("GO_WP_SEO_FEED", "off")
	svc := &Service{}
	if err := svc.refreshFeed(context.Background(), "p-1", "https://e.com", dir, nil, "", string(i18n.SiteLangURLModeOff)); err != nil {
		t.Fatalf("关闭开关时刷新应成功（只删除），实际: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("关闭开关后既有 feed 应被删除: %v", err)
	}

	// 幂等：文件不存在时再关一次也不报错。
	if err := svc.refreshFeed(context.Background(), "p-1", "https://e.com", dir, nil, "", string(i18n.SiteLangURLModeOff)); err != nil {
		t.Fatalf("关闭开关的刷新应幂等，实际: %v", err)
	}
	// 空目录/空工程直接跳过（与 sitemap 同约定），不查库也不报错。
	t.Setenv("GO_WP_SEO_FEED", "")
	if err := svc.refreshFeed(context.Background(), "", "https://e.com", dir, nil, "", string(i18n.SiteLangURLModeOff)); err != nil {
		t.Fatalf("空工程应跳过: %v", err)
	}
}
