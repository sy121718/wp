package productlist

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestFragmentQueryCarriesLang(t *testing.T) {
	p := &Props{LinkPrefix: "/products/"}
	ctx := &core.RenderContext{ProjectID: "proj-1", Lang: "en-US"}
	q := catalogQuery("list1", p, ctx)
	if !strings.Contains(q, "projectId=proj-1") || !strings.Contains(q, "lang=en-US") {
		t.Fatalf("实例配置应同时带工程与语言，实际 %q", q)
	}
}

func TestFragmentQueryOmitsEmptyLang(t *testing.T) {
	p := &Props{LinkPrefix: "/products/"}
	ctx := &core.RenderContext{ProjectID: "proj-1"}
	q := catalogQuery("list1", p, ctx)
	if strings.Contains(q, "lang=") {
		t.Fatalf("空语言不该拼出 lang 参数，实际 %q", q)
	}
}

func TestBuildViewFragmentQueryCarriesLang(t *testing.T) {
	coll := &fakeCollection{items: []map[string]any{{"name": "衬衫", "slug": "shirt"}}}
	ctx := &core.RenderContext{Collection: coll, ProjectID: "proj-1", Lang: "en-US"}

	p := decodePropsOf(t, map[string]any{
		"collectionSource": "content:product",
		"titleField":       "item.name",
		"linkField":        "item.slug",
		"linkPrefix":       "/products/",
	})
	view, err := BuildView(nodeOf(t, nil), &p, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !strings.Contains(view.DataQuery, "lang=en-US") {
		t.Fatalf("片段请求 URL 应带语言，实际 %q", view.DataQuery)
	}
}
