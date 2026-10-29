package searchresults

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

func TestBuildViewIncludesLangAndProject(t *testing.T) {
	v := BuildView(&Props{Limit: 5}, "proj-1", "en-US")
	if v.Notice != "" {
		t.Fatalf("unexpected notice: %q", v.Notice)
	}
	for _, want := range []string{"projectId=proj-1", "lang=en-US", "limit=5", "/api/site/search?"} {
		if !strings.Contains(v.BaseFragmentURL, want) {
			t.Fatalf("BaseFragmentURL %q missing %q", v.BaseFragmentURL, want)
		}
	}
}

func TestBuildViewOmitsEmptyLang(t *testing.T) {
	v := BuildView(&Props{}, "proj-1", "")
	if strings.Contains(v.BaseFragmentURL, "lang=") {
		t.Fatalf("empty lang should not appear in URL: %q", v.BaseFragmentURL)
	}
}

func TestBuildViewMissingProjectNotice(t *testing.T) {
	v := BuildView(&Props{}, "", "zh-CN")
	if v.Notice == "" {
		t.Fatal("missing project should set notice")
	}
}

// 冷启动沿用片段入口合并地址栏查询的能力，模板不执行 CSP 禁止的 JS 表达式。
func TestSearchWidgetRendersWithoutEvaluatedAttributes(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := set.GetTemplate("search_widget")
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	err = tpl.Execute(&output, nil, struct {
		Classes  string
		CustomID string
		V        View
	}{V: BuildView(&Props{}, "proj-1", "zh-CN")})
	if err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, want := range []string{`role="search"`, `method="get"`, `name="q"`, `data-site-view="search"`, `aria-live="polite"`, "/api/site/search?"} {
		if !strings.Contains(html, want) {
			t.Fatalf("搜索产物缺少 %q", want)
		}
	}
	for _, forbidden := range []string{"hx-", "load[", "js:", "/_fragments/"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("搜索产物仍需动态求值: %q", forbidden)
		}
	}
}
