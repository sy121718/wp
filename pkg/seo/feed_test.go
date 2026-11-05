package seo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rssParse(t *testing.T, out string) rssRoot {
	t.Helper()
	var root rssRoot
	if err := xml.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("feed 不是合法 XML: %v\n%s", err, out)
	}
	return root
}

func testItems() []FeedItem {
	return []FeedItem{
		{Title: "第二篇 & 标题", Link: "https://e.com/b", Description: "摘要 <b>二</b>",
			PubDate: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)},
		{Title: "第一篇", Link: "https://e.com/a", Description: "摘要一",
			PubDate: time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)},
	}
}

func TestBuildRSSStructure(t *testing.T) {
	out, err := BuildRSS(FeedChannel{Title: "示例站", Link: "https://e.com", Description: "站点描述", Language: "zh-CN"},
		testItems(), 0)
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	if !strings.HasPrefix(out, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n") {
		t.Fatalf("缺少 XML 声明: %q", out[:40])
	}
	root := rssParse(t, out)
	if root.Version != "2.0" {
		t.Fatalf("rss 版本应为 2.0，实际 %q", root.Version)
	}
	ch := root.Channel
	if ch.Title != "示例站" || ch.Link != "https://e.com" || ch.Description != "站点描述" {
		t.Fatalf("channel 必需元素不对: %+v", ch)
	}
	if ch.Language != "zh-CN" {
		t.Fatalf("channel language 应为 zh-CN，实际 %q", ch.Language)
	}
	if len(ch.Items) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(ch.Items))
	}

	if ch.Items[0].Link != "https://e.com/b" || ch.Items[1].Link != "https://e.com/a" {
		t.Fatalf("条目未按发布时间倒序: %+v", ch.Items)
	}
	if ch.Items[0].GUID.Value != "https://e.com/b" || ch.Items[0].GUID.IsPermaLink != "true" {
		t.Fatalf("guid 应为条目链接且 isPermaLink=true: %+v", ch.Items[0].GUID)
	}
	if ch.Items[0].PubDate != "Mon, 14 Sep 2026 10:00:00 +0000" {
		t.Fatalf("pubDate 不是 RFC822 格式: %q", ch.Items[0].PubDate)
	}
	if ch.LastBuildDate != "Mon, 14 Sep 2026 10:00:00 +0000" {
		t.Fatalf("lastBuildDate 应取条目里的最大发布时间: %q", ch.LastBuildDate)
	}

	if !strings.Contains(out, "第二篇 &amp; 标题") {
		t.Fatalf("标题里的 & 未转义:\n%s", out)
	}
	if ch.Items[0].Description != "摘要 <b>二</b>" {
		t.Fatalf("摘要经 XML 往返应原样还原，实际 %q", ch.Items[0].Description)
	}
}

func TestBuildRSSChannelFallback(t *testing.T) {
	out, err := BuildRSS(FeedChannel{Link: "https://e.com"}, nil, 0)
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	ch := rssParse(t, out).Channel
	if ch.Title != "https://e.com" {
		t.Fatalf("空 title 应回退 Link，实际 %q", ch.Title)
	}
	if ch.Description != "https://e.com" {
		t.Fatalf("空 description 应回退 title，实际 %q", ch.Description)
	}
	if len(ch.Items) != 0 {
		t.Fatalf("没有条目时不应凭空产出条目，实际 %d", len(ch.Items))
	}
}

func TestBuildRSSFilterSortTruncate(t *testing.T) {
	base := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	items := []FeedItem{
		{Title: "无链接", Link: "   "},
		{Title: "A", Link: "https://e.com/a", PubDate: base},
		{Title: "A 重复", Link: "https://e.com/a", PubDate: base},
		{Title: "B", Link: "https://e.com/b", PubDate: base},
		{Title: "C", Link: "https://e.com/c", PubDate: base.Add(-time.Hour)},
	}
	out, err := BuildRSS(FeedChannel{Title: "t", Link: "https://e.com"}, items, 2)
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	ch := rssParse(t, out).Channel
	if len(ch.Items) != 2 {
		t.Fatalf("上限 2 应只输出 2 条，实际 %d", len(ch.Items))
	}
	if ch.Items[0].Link != "https://e.com/a" || ch.Items[1].Link != "https://e.com/b" {
		t.Fatalf("同一时刻应按链接升序稳定排序: %+v", ch.Items)
	}
}

func TestBuildRSSDeterministic(t *testing.T) {
	ch := FeedChannel{Title: "示例站", Link: "https://e.com", Description: "d"}
	items := testItems()
	first, err := BuildRSS(ch, items, 0)
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}

	flipped := []FeedItem{items[1], items[0]}
	second, err := BuildRSS(ch, flipped, 0)
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	if first != second {
		t.Fatalf("同一批条目换顺序后字节不同:\n%s\n----\n%s", first, second)
	}
	third, _ := BuildRSS(ch, items, 0)
	if first != third {
		t.Fatal("同输入两次生成字节不同（含当前时刻之类的非确定值？）")
	}
}

func TestWriteFeedAndRemoveFeed(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFeed(dir, FeedChannel{Title: "示例站", Link: "https://e.com"}, testItems(), 0); err != nil {
		t.Fatalf("WriteFeed: %v", err)
	}
	path := filepath.Join(dir, FeedFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 feed 失败: %v", err)
	}
	if !strings.Contains(string(data), "<rss version=\"2.0\">") {
		t.Fatalf("写出的不是 RSS 2.0：%s", string(data))
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("原子写不应留下临时文件: %v", err)
	}

	if err := RemoveFeed(dir); err != nil {
		t.Fatalf("RemoveFeed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("RemoveFeed 后文件应不存在: %v", err)
	}

	if err := RemoveFeed(dir); err != nil {
		t.Fatalf("RemoveFeed 应幂等: %v", err)
	}
}

func TestWriteFeedEmptyDir(t *testing.T) {
	if err := WriteFeed("   ", FeedChannel{Title: "t", Link: "https://e.com"}, nil, 0); err != nil {
		t.Fatalf("空目录应跳过而不是报错: %v", err)
	}
	if err := RemoveFeed(""); err != nil {
		t.Fatalf("空目录删除应跳过而不是报错: %v", err)
	}
}

func TestFeedEnabled(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", true}, {"on", true}, {"true", true}, {"1", true},
		{"off", false}, {"OFF", false}, {"0", false}, {"false", false}, {"no", false},
	}
	for _, c := range cases {
		t.Setenv(feedEnabledEnv, c.raw)
		if got := FeedEnabled(); got != c.want {
			t.Errorf("GO_WP_SEO_FEED=%q 应得 %v，实际 %v", c.raw, c.want, got)
		}
	}
}

func TestFeedLimit(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", DefaultFeedLimit}, {"5", 5}, {"100", 100},
		{"abc", DefaultFeedLimit}, {"0", DefaultFeedLimit}, {"-3", DefaultFeedLimit},
	}
	for _, c := range cases {
		t.Setenv(feedLimitEnv, c.raw)
		if got := FeedLimit(); got != c.want {
			t.Errorf("GO_WP_SEO_FEED_LIMIT=%q 应得 %d，实际 %d", c.raw, c.want, got)
		}
	}
}
