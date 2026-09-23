package pipeline

// site_link_absolute_test.go — 「站内链接一律绝对地址」的门禁。
//
// 判据收敛在 AbsoluteSiteURL 一处：站内链接的每个产出口（导航菜单本地化、
// 组件手填站内链接、站点槽位地址）都必须经它。漏掉任何一处，表现是
// 「多数链接绝对、少数仍相对」—— 站点独占域名根时两者等价、肉眼看不出区别，
// 只有跨域 / 镜像 / 被抓取到别处渲染时才暴露，属于最难发现的一类不一致。

import (
	"strings"
	"testing"
)

// TestAbsoluteSiteURL 原语的边界。
func TestAbsoluteSiteURL(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.example.com")
	cases := []struct{ in, want string }{
		{"/", "https://shop.example.com/"},
		{"/shop", "https://shop.example.com/shop"},
		{"/product/x", "https://shop.example.com/product/x"},
		// 已绝对 / 协议相对 / 外部 scheme：一律不动（补基址会改成错的地址）。
		{"https://other.example.com/a", "https://other.example.com/a"},
		{"http://other.example.com/a", "http://other.example.com/a"},
		{"//cdn.example.com/a", "//cdn.example.com/a"},
		{"mailto:a@b.c", "mailto:a@b.c"},
		{"#anchor", "#anchor"},
		{"?page=2", "?page=2"},
		{"relative/x", "relative/x"},
		{"", ""},
	}
	for _, c := range cases {
		if got := AbsoluteSiteURL(c.in); got != c.want {
			t.Errorf("AbsoluteSiteURL(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestAbsoluteSiteURLWithoutBase 未配置站点基址时原样返回（不猜域名）。
func TestAbsoluteSiteURLWithoutBase(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "")
	if got := AbsoluteSiteURL("/shop"); got != "/shop" {
		t.Errorf("未配置基址时应原样返回，实际 %q", got)
	}
}

// TestAbsoluteSiteURLKeepsBasePathPrefix 基址带路径前缀时前缀不能被吞掉。
func TestAbsoluteSiteURLKeepsBasePathPrefix(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://example.com/shop/")
	if got := AbsoluteSiteURL("/cart"); got != "https://example.com/shop/cart" {
		t.Errorf("带前缀的基址应保留前缀，实际 %q", got)
	}
}

// TestLocalizeMenuURLIsAbsolute 导航菜单本地化的输出必须是绝对地址。
//
// 这条覆盖的是产物里**数量最大**的一类链接（每个页面都有整套菜单），
// 也是唯一走 LocalizeMenuURLWith 这条独立本地化路径的链接来源。
func TestLocalizeMenuURLIsAbsolute(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.example.com")
	rule := NewLangURLRule(false, false, "en-AU", nil)
	cases := []struct{ raw, want string }{
		{"/shop", "https://shop.example.com/shop"},
		{"/product_category/kuz", "https://shop.example.com/product_category/kuz"},
		{"/", "https://shop.example.com/"},
		// 外部链接原样返回。
		{"https://other.example.com/a", "https://other.example.com/a"},
		{"//cdn.example.com/a", "//cdn.example.com/a"},
	}
	for _, c := range cases {
		got := LocalizeMenuURLWith(rule, []string{"en-AU"}, "en-AU", c.raw)
		if got != c.want {
			t.Errorf("LocalizeMenuURLWith(%q) = %q，期望 %q", c.raw, got, c.want)
		}
		// "//host/x" 是**协议相对的外部地址**，合法且以 / 开头，不能算「站内根相对」。
		if strings.HasPrefix(got, "/") && !strings.HasPrefix(got, "//") {
			t.Errorf("LocalizeMenuURLWith(%q) 产出了站内根相对地址 %q —— 导航链接必须绝对", c.raw, got)
		}
	}
}
