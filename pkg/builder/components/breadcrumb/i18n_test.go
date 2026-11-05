package breadcrumb

import (
	"testing"

	"go_wp/pkg/builder/core"
)

func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

func TestBuildViewLocalizesAuthorItems(t *testing.T) {
	ctx := &core.RenderContext{}
	ctx.SetSiteLinkResolver(siteLinkOf)

	v := BuildView(&Props{Items: []Item{
		{Label: "首页", URL: "/"},
		{Label: "关于", URL: "/about"},
	}}, ctx)
	if len(v.Items) != 2 {
		t.Fatalf("应输出两项，实际 %+v", v.Items)
	}
	if v.Items[0].URL != "/en/" || v.Items[1].URL != "/en/about" {
		t.Fatalf("作者手填的层级项应加语言前缀，实际 %+v", v.Items)
	}

	v.JSONLD = ""
	if !v.Visible {
		t.Fatalf("作者手填层级应可见")
	}
}

func TestBuildViewDerivedItemsNotDoublePrefixed(t *testing.T) {
	ctx := &core.RenderContext{CurrentPath: "/en/about"}
	ctx.SetSiteLinkResolver(siteLinkOf)

	v := BuildView(&Props{}, ctx)
	if !v.Visible || len(v.Items) < 2 {
		t.Fatalf("派生层级应可见，实际 %+v", v.Items)
	}
	last := v.Items[len(v.Items)-1]
	if last.URL != "/en/about" {
		t.Fatalf("派生项不应被二次前缀，实际 %q", last.URL)
	}
}

func TestBuildViewWithoutSiteLink(t *testing.T) {
	v := BuildView(&Props{Items: []Item{{Label: "首页", URL: "/"}, {Label: "关于", URL: "/about"}}}, nil)
	if v.Items[1].URL != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", v.Items[1].URL)
	}
}
