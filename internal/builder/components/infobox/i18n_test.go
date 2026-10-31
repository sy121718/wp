package infobox

import "testing"

func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

func TestBuildViewLocalizesSiteLink(t *testing.T) {
	v := BuildView(&Props{Link: "/about", Label: "了解"}, siteLinkOf)
	if !v.HasLink || v.Link != "/en/about" {
		t.Fatalf("整卡链接应加语言前缀，实际 %+v", v)
	}

	plain := BuildView(&Props{Link: "/about"}, nil)
	if plain.Link != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", plain.Link)
	}

	ext := BuildView(&Props{Link: "https://example.com/x"}, siteLinkOf)
	if ext.Link != "https://example.com/x" {
		t.Fatalf("外链不该被改写，实际 %q", ext.Link)
	}
}

func TestBuildViewNoLinkStaysEmpty(t *testing.T) {
	v := BuildView(&Props{Title: "无链接"}, siteLinkOf)
	if v.HasLink || v.Link != "" {
		t.Fatalf("未填链接不应输出链接，实际 %+v", v)
	}
}
