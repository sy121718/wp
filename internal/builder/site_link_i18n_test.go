package builder

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

const siteLinkDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "链接", "description": "链接"}},
  "root": [
    {"id": "ib1", "type": "core.infobox", "props": {"title": "关于", "link": "/about"}},
    {"id": "q1", "type": "core.quote", "props": {"text": "引用", "author": "某人", "source": "/about"}},
    {"id": "bc1", "type": "core.breadcrumb", "props": {"items": [{"label": "首页", "url": "/"}, {"label": "关于", "url": "/about"}]}},
    {"id": "g1", "type": "core.gallery", "props": {"items": [{"url": "/storage/a.jpg", "link": "/about"}]}}
  ]
}`

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

func TestCompileLocalizesComponentSiteLinks(t *testing.T) {
	html := compileSiteLinkDoc(t, WithSiteLinkResolver(func(p string) string {
		if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
			return "/en" + p
		}
		return p
	}))

	if n := strings.Count(html, `href="/en/about"`); n < 4 {
		t.Fatalf("站内链接应全部本地化（期望 ≥4 处 /en/about，实际 %d 处）\nHTML=%s", n, html)
	}
	if strings.Contains(html, `href="/about"`) {
		t.Fatalf("不得残留未本地化的站内链接\nHTML=%s", html)
	}
}

func TestCompileWithoutResolverKeepsLogicalPath(t *testing.T) {
	html := compileSiteLinkDoc(t)
	if !strings.Contains(html, `href="/about"`) {
		t.Fatalf("未注入解析器时应保持逻辑路径\nHTML=%s", html)
	}
	if strings.Contains(html, "/en/about") {
		t.Fatalf("未注入解析器时不得凭空出现语言前缀\nHTML=%s", html)
	}
}

func TestCompileResolverNilSafe(t *testing.T) {
	var ctx *core.RenderContext
	if got := ctx.ResolveSiteLink("/about"); got != "/about" {
		t.Fatalf("未注入解析器时应原样返回，实际 %q", got)
	}
}
