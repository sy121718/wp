package card

// i18n_test.go — 卡片按钮链接的站内本地化（审计 I18N-015）。
//
// 只测站内路径：外链 / 锚点 / 协议相对地址由 core.ResolveSiteLink 放行（core 侧有用例）。
// 未注入本地化器时原样输出 —— 单测里通常就是这种情况，也是「缺前缀是可见降级」的体现。

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
