package pipeline

// site_lang_published_test.go — 语言切换器的发布状态过滤（审计 I18N-021）。
//
// 这条 finding 的现象是「切换器里的某个语言点进去 404」。成因不在链接的拼法，
// 而在**判据**：SiteRouteEntries 只从「启用语言」推导路径，它不知道某个语言的页面
// 有没有构建成功。所以这里钉住的是判据本身 —— 未发布的语言不进切换器，
// 但当前语言必须留下。

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
		// 整套判断里最反直觉、也最容易被「顺手优化掉」的一条：
		// 少了它，用户在看 /en/about 时切换器里没有 en-US，会以为站点只有中文。
		//
		// 这里 en-US 自己**未发布**（stub 只认 /about），却在当前语言位置被留下 ——
		// 这正是「当前语言不参与过滤」与「其余语言按发布状态过滤」的分界。
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
