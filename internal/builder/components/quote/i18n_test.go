package quote

// i18n_test.go — 引文出处链接的站内本地化（审计 I18N-015）。
//
// 出处链接是作者手填的（ct:"text"）：可能是站内路径，也可能是书/文章的**外链**。
// 本地化器只处理站内相对路径，外链原样返回，所以那部分放行由 core.ResolveSiteLink 负责。

import "testing"

// siteLinkOf 模拟装配层注入的本地化器：只对站内路径加前缀。
func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

func TestBuildViewLocalizesSource(t *testing.T) {
	v := BuildView(&Props{Text: "引用", Author: "某人", Source: "/about"}, siteLinkOf)
	if !v.HasSource || v.Source != "/en/about" {
		t.Fatalf("站内出处链接应加语言前缀，实际 %+v", v)
	}

	plain := BuildView(&Props{Text: "引用", Author: "某人", Source: "/about"}, nil)
	if plain.Source != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", plain.Source)
	}

	ext := BuildView(&Props{Text: "引用", Author: "某人", Source: "https://example.com/book"}, siteLinkOf)
	if ext.Source != "https://example.com/book" {
		t.Fatalf("外链出处不该被改写，实际 %q", ext.Source)
	}
}
