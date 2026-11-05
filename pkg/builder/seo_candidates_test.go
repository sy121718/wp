package builder

import "testing"

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

	if empty := AppendSEOCandidates(&Page{}, nil); len(empty) != 0 {
		t.Errorf("空 SEO 字段不该产生候选，实际 %+v", empty)
	}

	p.Settings.SEO.FocusKeyword = "关键词"
	for _, c := range CollectContentCandidatesForDocument(p, nil) {
		if c.Context == "page.seo.focusKeyword" {
			t.Error("focusKeyword 不进产物，不该进译文候选")
		}
	}
}
