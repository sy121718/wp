package scoring

import (
	"strings"
	"testing"
)

// title_unique_test.go — 编辑期 title 唯一性检查的单测（审计 SEO-018 轻量版）。
//
// 这一组的重点是**反例**：重复要被检出并列出全部冲突页面，不重复时一个都不许报。
// 会误报的检查最后都会被当成噪音忽略掉（发布侧 SEO-019 的同组测试也是这个思路）。

// TestDuplicateTitlesListsAllConflictingPages 重复被检出且列出全部命中页面。
func TestDuplicateTitlesListsAllConflictingPages(t *testing.T) {
	entries := []TitleEntry{
		{Title: "纯棉 T 恤", Page: "/products/tee-a", ID: "a"},
		{Title: "纯棉 T 恤", Page: "/products/tee-b", ID: "b"},
		{Title: "纯棉 T 恤", Page: "/products/tee-c", ID: "c"},
		{Title: "牛仔裤", Page: "/products/jeans", ID: "d"},
	}
	dups := DuplicateTitles("纯棉 T 恤", entries, "a")
	if len(dups) != 2 {
		t.Fatalf("应检出 2 个冲突页面（排除自身），实际 %d：%+v", len(dups), dups)
	}
	if dups[0].Page != "/products/tee-b" || dups[1].Page != "/products/tee-c" {
		t.Fatalf("冲突页面应稳定排序，实际 %+v", dups)
	}
	msg := DuplicateTitleMessage("纯棉 T 恤", dups)
	for _, want := range []string{"/products/tee-b", "/products/tee-c", "2 个页面"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("结论里应列出 %q，实际 %q", want, msg)
		}
	}
}

// TestDuplicateTitlesNoFalsePositive 不重复 / 空标题 / 只有自身 都不报。
func TestDuplicateTitlesNoFalsePositive(t *testing.T) {
	entries := []TitleEntry{
		{Title: "纯棉 T 恤", Page: "/products/tee-a", ID: "a"},
		{Title: "牛仔裤", Page: "/products/jeans", ID: "d"},
	}
	if dups := DuplicateTitles("纯棉 T 恤", entries, "a"); len(dups) != 0 {
		t.Fatalf("只有自身命中时不应报重复，实际 %+v", dups)
	}
	if dups := DuplicateTitles("", entries, "a"); len(dups) != 0 {
		t.Fatalf("空标题不应报重复（那是 title_present 的事），实际 %+v", dups)
	}
	if dups := DuplicateTitles("   ", entries, "a"); len(dups) != 0 {
		t.Fatalf("纯空白标题不应报重复，实际 %+v", dups)
	}
	if dups := DuplicateTitles("新商品", entries, "x"); len(dups) != 0 {
		t.Fatalf("没有重复时不应报，实际 %+v", dups)
	}
	if msg := DuplicateTitleMessage("新商品", nil); msg != "" {
		t.Fatalf("无冲突时不应产生说明，实际 %q", msg)
	}
}

// TestDuplicateTitlesMatchesPublishSideWording 与发布侧同一句式的结论。
//
// 发布侧（publication/service/seo_audit.go）的句式是
// 「重复的 title（X）出现在 N 个页面：/a、/b」；编辑期必须说同一句话，
// 否则运营会把它当成两个问题。
func TestDuplicateTitlesMatchesPublishSideWording(t *testing.T) {
	dups := []TitleEntry{
		{Title: "首页", Page: "/"},
		{Title: "首页", Page: "/home"},
	}
	got := DuplicateTitleMessage("首页", dups)
	want := "重复的 title（首页）出现在 2 个页面：/、/home"
	if got != want {
		t.Fatalf("结论句式应与发布侧一致：实际 %q，期望 %q", got, want)
	}
}

// TestDuplicateTitlesDedupesSamePage 同一页面在索引里出现两次时只列一次。
func TestDuplicateTitlesDedupesSamePage(t *testing.T) {
	entries := []TitleEntry{
		{Title: "关于", Page: "/about", ID: "p1"},
		{Title: "关于", Page: "/about", ID: "inst-1"},
		{Title: "关于", Page: "/contact", ID: "p2"},
	}
	dups := DuplicateTitles("关于", entries, "self")
	if len(dups) != 2 {
		t.Fatalf("同一路径只应列一次，实际 %d：%+v", len(dups), dups)
	}
}
