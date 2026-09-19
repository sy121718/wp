package productlist

// fragment_lang_test.go — 列表片段请求 URL 必须带语言（I18N-011）。
//
// 片段语言**只从 lang 参数来**（端点不读 Accept-Language、不读语言 cookie）：
// 实例配置里没有 lang，访问面首屏（静态产物）的 hx-get 就只会带工程 id，
// 于是英文站刷新一次列表就出中文 —— 页面看起来正常，只是语言变了。
//
// 空语言（单语言站点）不带该参数：空值只让片段多做一次无用判断。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestFragmentQueryCarriesLang 实例配置（片段请求的固定部分）带构建语言。
func TestFragmentQueryCarriesLang(t *testing.T) {
	p := &Props{LinkPrefix: "/products/"}
	ctx := &core.RenderContext{ProjectID: "proj-1", Lang: "en-US"}
	q := fragmentQuery("list1", p, ctx)
	if !strings.Contains(q, "projectId=proj-1") || !strings.Contains(q, "lang=en-US") {
		t.Fatalf("实例配置应同时带工程与语言，实际 %q", q)
	}
}

// TestFragmentQueryOmitsEmptyLang 语言为空时不拼该参数。
func TestFragmentQueryOmitsEmptyLang(t *testing.T) {
	p := &Props{LinkPrefix: "/products/"}
	ctx := &core.RenderContext{ProjectID: "proj-1"}
	q := fragmentQuery("list1", p, ctx)
	if strings.Contains(q, "lang=") {
		t.Fatalf("空语言不该拼出 lang 参数，实际 %q", q)
	}
}

// TestBuildViewFragmentQueryCarriesLang 组装路径同样带上（构建期首屏用的就是这一份）。
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
	if !strings.Contains(view.FragmentQuery, "lang=en-US") {
		t.Fatalf("片段请求 URL 应带语言，实际 %q", view.FragmentQuery)
	}
}
