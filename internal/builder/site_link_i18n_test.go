package builder

// site_link_i18n_test.go — 站内链接本地化的**接线**回归（审计 I18N-015）。
//
// 组件单测只证明「拿到解析器时会用」；真正容易静默失效的是装配那一段 ——
// jetview 把 ctx.ResolveSiteLink 传进各组件 BuildView。少传一个参数、少包一层闭包，
// 编译照过、单测照绿，线上只是「英文站里那个链接跳回中文页」。
//
// 所以这里走完整 Compile：注入解析器 → 产物里必须出现带前缀的 href；
// 不注入（单语言站点的真实形态）→ 产物与接入前一致，一个语言前缀都不许出现。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// siteLinkDoc 四个「作者可填站内链接」的组件各一个节点（本轮接线的全部入口）。
const siteLinkDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "链接", "description": "链接"}},
  "root": [
    {"id": "ib1", "type": "core.infobox", "props": {"title": "关于", "link": "/about"}},
    {"id": "q1", "type": "core.quote", "props": {"text": "引用", "author": "某人", "source": "/about"}},
    {"id": "bc1", "type": "core.breadcrumb", "props": {"items": [{"label": "首页", "url": "/"}, {"label": "关于", "url": "/about"}]}},
    {"id": "g1", "type": "core.gallery", "props": {"items": [{"url": "/storage/a.jpg", "link": "/about"}]}}
  ]
}`

// compileSiteLinkDoc 编译站内链接用例文档。
func compileSiteLinkDoc(t *testing.T, opts ...CompileOption) string {
	t.Helper()
	p, err := ParsePage([]byte(siteLinkDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts = append([]CompileOption{WithComponentSet(i18nTestComponentSet(t))}, opts...)
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res.HTML
}

// TestCompileLocalizesComponentSiteLinks 注入解析器后，四个组件的站内链接都带语言前缀。
func TestCompileLocalizesComponentSiteLinks(t *testing.T) {
	html := compileSiteLinkDoc(t, WithSiteLinkResolver(func(p string) string {
		if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
			return "/en" + p
		}
		return p
	}))

	// 四个组件的链接都必须是本地化后的地址（infobox / quote / breadcrumb / gallery）。
	if n := strings.Count(html, `href="/en/about"`); n < 4 {
		t.Fatalf("站内链接应全部本地化（期望 ≥4 处 /en/about，实际 %d 处）\nHTML=%s", n, html)
	}
	if strings.Contains(html, `href="/about"`) {
		t.Fatalf("不得残留未本地化的站内链接\nHTML=%s", html)
	}
}

// TestCompileWithoutResolverKeepsLogicalPath 不注入解析器（单语言站点形态）：链接保持逻辑路径。
func TestCompileWithoutResolverKeepsLogicalPath(t *testing.T) {
	html := compileSiteLinkDoc(t)
	if !strings.Contains(html, `href="/about"`) {
		t.Fatalf("未注入解析器时应保持逻辑路径\nHTML=%s", html)
	}
	if strings.Contains(html, "/en/about") {
		t.Fatalf("未注入解析器时不得凭空出现语言前缀\nHTML=%s", html)
	}
}

// TestCompileResolverNilSafe core.ResolveSiteLink 的 nil 接收者安全（独立编译 / 单测路径）。
func TestCompileResolverNilSafe(t *testing.T) {
	var ctx *core.RenderContext
	if got := ctx.ResolveSiteLink("/about"); got != "/about" {
		t.Fatalf("未注入解析器时应原样返回，实际 %q", got)
	}
}
