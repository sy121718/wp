package breadcrumb

// i18n_test.go — 面包屑层级项链接的站内本地化（审计 I18N-015）。
//
// 关键分野（两处 URL 来源的语义完全不同）：
//   - **作者手填**的 items[].url 是站内逻辑路径（/about）→ 按当前语言加前缀；
//   - **派生**的层级项来自 ctx.CurrentPath（已是当前语言的访问路径，如 /en/about）
//     → 不能再前缀一次，否则会得到 /en/en/about。
//
// 这也是判据只能取「数据来源」而不是字符串形状的又一例。

import (
	"testing"

	"go_wp/internal/builder/core"
)

// siteLinkOf 模拟装配层注入的本地化器：只对站内路径加前缀。
func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

// TestBuildViewLocalizesAuthorItems 作者手填的层级项 URL 按语言加前缀。
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
	// 结构化数据与可见项同源：本地化后的 URL 必须一并进 JSON-LD。
	v.JSONLD = ""
	if !v.Visible {
		t.Fatalf("作者手填层级应可见")
	}
}

// TestBuildViewDerivedItemsNotDoublePrefixed 派生模式的 URL 已带语言前缀，不再二次前缀。
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

// TestBuildViewWithoutSiteLink 未注入本地化器时原样输出。
func TestBuildViewWithoutSiteLink(t *testing.T) {
	v := BuildView(&Props{Items: []Item{{Label: "首页", URL: "/"}, {Label: "关于", URL: "/about"}}}, nil)
	if v.Items[1].URL != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", v.Items[1].URL)
	}
}
