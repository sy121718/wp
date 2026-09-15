package builder

import "testing"

// TestAppendSEOCandidates SEO 文本字段进候选集合（审计 I18N-014）。
//
// 这条 finding 的验收里最容易漏的是「工作台看得到」：构建期与工作台用不同的收集函数
// （文档级 / 只扫 AST），只在构建期补候选的话功能是半截的 —— 译文取得到，但没人能填。
func TestAppendSEOCandidates(t *testing.T) {
	p := &Page{Settings: PageSettings{SEO: SEO{Title: "中文标题", Description: "中文描述"}}}
	got := CollectContentCandidatesForDocument(p, nil)
	seen := map[string]string{}
	for _, c := range got {
		seen[c.Context] = c.Source
	}
	if seen[SEOTitleContext] != "中文标题" {
		t.Errorf("SEO 标题应进候选集合，实际 %q", seen[SEOTitleContext])
	}
	if seen[SEODescriptionContext] != "中文描述" {
		t.Errorf("SEO 描述应进候选集合，实际 %q", seen[SEODescriptionContext])
	}

	// 空值不进候选：空串的 hash 没有意义，写进候选只会让 Misses 虚增。
	if empty := AppendSEOCandidates(&Page{}, nil); len(empty) != 0 {
		t.Errorf("空 SEO 字段不该产生候选，实际 %+v", empty)
	}

	// focusKeyword 是评分输入、不进产物，即使填了也不该进候选。
	p.Settings.SEO.FocusKeyword = "关键词"
	for _, c := range CollectContentCandidatesForDocument(p, nil) {
		if c.Context == "page.seo.focusKeyword" {
			t.Error("focusKeyword 不进产物，不该进译文候选")
		}
	}
}
