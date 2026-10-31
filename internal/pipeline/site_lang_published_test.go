package pipeline

import "testing"

func TestFilterPublishedLocales(t *testing.T) {
	entries := []SiteRouteEntry{
		{Lang: "zh-CN", Path: "/about"},
		{Lang: "en-US", Path: "/en/about"},
	}

	t.Run("未接入校验时原样返回", func(t *testing.T) {
		got := filterPublishedLocales(entries, "zh-CN", nil)
		if len(got) != 2 {
			t.Fatalf("published 为 nil 时不应过滤（否则未接入校验会改变产物），实际 %d 条", len(got))
		}
	})

	t.Run("未发布的语言被跳过", func(t *testing.T) {
		published := func(p string) bool { return p != "/en/about" }
		got := filterPublishedLocales(entries, "zh-CN", published)
		if len(got) != 1 || got[0].Lang != "zh-CN" {
			t.Fatalf("未发布的 en-US 应被跳过，实际 %+v", got)
		}
	})

	t.Run("当前语言不参与过滤", func(t *testing.T) {

		published := func(p string) bool { return p == "/about" }
		got := filterPublishedLocales(entries, "en-US", published)
		if len(got) != 2 {
			t.Fatalf("当前语言未发布也应保留（zh-CN 已发布同样保留），实际 %+v", got)
		}
	})

	t.Run("全部未发布时只剩当前语言", func(t *testing.T) {
		published := func(string) bool { return false }
		got := filterPublishedLocales(entries, "zh-CN", published)
		if len(got) != 1 || got[0].Lang != "zh-CN" {
			t.Fatalf("实际 %+v", got)
		}
	})
}
