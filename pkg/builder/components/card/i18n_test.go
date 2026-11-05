package card

import "testing"

func TestBuildViewLocalizesSiteLink(t *testing.T) {
	p := &Props{ButtonLink: "/about"}
	view := BuildView(p, func(s string) string { return "/en" + s })
	if view.ButtonLink != "/en/about" {
		t.Fatalf("按钮链接应加语言前缀，实际 %q", view.ButtonLink)
	}
	plain := BuildView(p, nil)
	if plain.ButtonLink != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", plain.ButtonLink)
	}
}
