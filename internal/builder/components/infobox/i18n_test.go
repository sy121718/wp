package infobox

// i18n_test.go — 信息框链接的站内本地化（审计 I18N-015）。
//
// 只测站内路径：外链 / 锚点 / 协议相对地址由 core.ResolveSiteLink 放行（core 侧有用例）。
// 未注入本地化器时原样输出 —— 单测 / 独立编译里通常就是这种情况。

import "testing"

// siteLinkOf 模拟装配层注入的本地化器：只对站内路径加前缀。
func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

func TestBuildViewLocalizesSiteLink(t *testing.T) {
	v := BuildView(&Props{Link: "/about", BtnText: "了解"}, siteLinkOf)
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

// TestBuildViewNoLinkStaysEmpty 没填链接时不上链接、也不输出空 href。
func TestBuildViewNoLinkStaysEmpty(t *testing.T) {
	v := BuildView(&Props{Title: "无链接"}, siteLinkOf)
	if v.HasLink || v.Link != "" {
		t.Fatalf("未填链接不应输出链接，实际 %+v", v)
	}
}
