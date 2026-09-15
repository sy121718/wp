package scoring

// linksuggest_test.go — 内链建议引擎的边界钉子：空候选 / 全排除 / 自身排除 /
// 已链接排除 / 排序与并列 tie-break / 截断。

import (
	"fmt"
	"reflect"
	"testing"
)

func TestInternalLinkSuggestionsNilDoc(t *testing.T) {
	if got := InternalLinkSuggestions(nil, []LinkCandidate{{ID: "a", URL: "/a"}}, 0); len(got) != 0 {
		t.Fatalf("nil 文档应返回空，got %v", got)
	}
}

func TestInternalLinkSuggestionsEmptyCandidates(t *testing.T) {
	doc := &LinkDocument{SelfID: "self", FocusKeyword: "咖啡"}
	if got := InternalLinkSuggestions(doc, nil, 0); len(got) != 0 {
		t.Fatalf("空候选应返回空，got %v", got)
	}
}

func TestInternalLinkSuggestionsExcludesMissingURL(t *testing.T) {
	// 全部候选都没有真实路径 → 全排除（死链防线）。
	doc := &LinkDocument{SelfID: "self"}
	cands := []LinkCandidate{
		{ID: "a", Title: "无路径", URL: "   "},
		{ID: "b", Title: "空路径"},
	}
	if got := InternalLinkSuggestions(doc, cands, 0); len(got) != 0 {
		t.Fatalf("无路径候选应全部排除，got %v", got)
	}
}

func TestInternalLinkSuggestionsExcludesSelf(t *testing.T) {
	doc := &LinkDocument{SelfID: "self", SelfURL: "/self"}
	cands := []LinkCandidate{
		{ID: "self", Title: "自己", URL: "/other"},    // id 命中自身
		{ID: "mirror", Title: "路径相同", URL: "/self"}, // URL 命中自身（id 不同的冗余闸）
		{ID: "ok", Title: "正常", URL: "/ok"},
	}
	got := InternalLinkSuggestions(doc, cands, 0)
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("应只保留正常候选，got %v", got)
	}
}

func TestInternalLinkSuggestionsExcludesLinked(t *testing.T) {
	doc := &LinkDocument{SelfID: "self", LinkedURLs: map[string]bool{"/linked": true}}
	cands := []LinkCandidate{
		{ID: "l", Title: "已链接", URL: "/linked"},
		{ID: "n", Title: "新目标", URL: "/new"},
	}
	got := InternalLinkSuggestions(doc, cands, 0)
	if len(got) != 1 || got[0].ID != "n" {
		t.Fatalf("已链接目标应排除，got %v", got)
	}
}

func TestInternalLinkSuggestionsRankByOverlap(t *testing.T) {
	doc := &LinkDocument{
		SelfID:       "self",
		FocusKeyword: "手冲咖啡",
		Tags:         []string{"器具"},
		Categories:   []string{"教程"},
	}
	cands := []LinkCandidate{
		{ID: "cat", Title: "仅分类", URL: "/cat", Categories: []string{"教程"}},
		{ID: "kw", Title: "关键词", URL: "/kw", FocusKeyword: "手冲咖啡"},
		{ID: "tag", Title: "标签", URL: "/tag", Tags: []string{"器具"}},
		{ID: "none", Title: "无重叠", URL: "/none"},
	}
	got := InternalLinkSuggestions(doc, cands, 0)
	wantOrder := []string{"kw", "tag", "cat", "none"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Fatalf("排序不符：位置 %d 期望 %s，got %s（%v）", i, want, got[i].ID, got)
		}
	}
	// 得分钉住：关键词 3、标签 2、分类 1、无重叠 0。
	wantScores := map[string]int{"kw": 3, "tag": 2, "cat": 1, "none": 0}
	for _, s := range got {
		if s.Score != wantScores[s.ID] {
			t.Fatalf("%s 得分期望 %d，got %d", s.ID, wantScores[s.ID], s.Score)
		}
	}
}

func TestInternalLinkSuggestionsTagAndCategoryStack(t *testing.T) {
	// 多个标签 / 分类交集累加：2 个标签（2*2）+ 1 个分类（1）= 5。
	doc := &LinkDocument{SelfID: "self", Tags: []string{"a", "b"}, Categories: []string{"c"}}
	cands := []LinkCandidate{
		{ID: "x", Title: "多交集", URL: "/x", Tags: []string{"a", "b"}, Categories: []string{"c"}},
	}
	got := InternalLinkSuggestions(doc, cands, 0)
	if len(got) != 1 || got[0].Score != 5 {
		t.Fatalf("交集应累加为 5 分，got %v", got)
	}
}

func TestInternalLinkSuggestionsTieBreakByTitle(t *testing.T) {
	// 并列分数按标题升序：同一输入两次调用结果一致（确定性）。
	doc := &LinkDocument{SelfID: "self"}
	cands := []LinkCandidate{
		{ID: "3", Title: "丙", URL: "/3"},
		{ID: "1", Title: "甲", URL: "/1"},
		{ID: "2", Title: "乙", URL: "/2"},
	}
	got := InternalLinkSuggestions(doc, cands, 0)
	var order []string
	for _, s := range got {
		order = append(order, s.Title)
	}
	// Unicode 码位序：丙(U+4E19) < 乙(U+4E59) < 甲(U+7532)。
	if !reflect.DeepEqual(order, []string{"丙", "乙", "甲"}) {
		t.Fatalf("并列应按标题升序，got %v", order)
	}
	again := InternalLinkSuggestions(doc, cands, 0)
	if !reflect.DeepEqual(got, again) {
		t.Fatal("同输入两次调用结果应一致")
	}
}

func TestInternalLinkSuggestionsLimit(t *testing.T) {
	doc := &LinkDocument{SelfID: "self"}
	var cands []LinkCandidate
	for i := 0; i < 10; i++ {
		cands = append(cands, LinkCandidate{ID: fmt.Sprintf("%d", i), Title: fmt.Sprintf("t%02d", i), URL: fmt.Sprintf("/%d", i)})
	}
	got := InternalLinkSuggestions(doc, cands, 3)
	if len(got) != 3 {
		t.Fatalf("limit=3 应截断到 3 条，got %d", len(got))
	}
	all := InternalLinkSuggestions(doc, cands, 0)
	if len(all) != 10 {
		t.Fatalf("limit<=0 不截断，got %d", len(all))
	}
}
