package pipeline

import (
	"strings"
	"testing"
)

func TestAbsoluteSiteURL(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.example.com")
	cases := []struct{ in, want string }{
		{"/", "https://shop.example.com/"},
		{"/shop", "https://shop.example.com/shop"},
		{"/product/x", "https://shop.example.com/product/x"},

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

func TestAbsoluteSiteURLWithoutBase(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "")
	if got := AbsoluteSiteURL("/shop"); got != "/shop" {
		t.Errorf("未配置基址时应原样返回，实际 %q", got)
	}
}

func TestAbsoluteSiteURLKeepsBasePathPrefix(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://example.com/shop/")
	if got := AbsoluteSiteURL("/cart"); got != "https://example.com/shop/cart" {
		t.Errorf("带前缀的基址应保留前缀，实际 %q", got)
	}
}

func TestLocalizeMenuURLIsAbsolute(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.example.com")
	rule := NewLangURLRule(false, false, "en-AU", nil)
	cases := []struct{ raw, want string }{
		{"/shop", "https://shop.example.com/shop"},
		{"/product_category/kuz", "https://shop.example.com/product_category/kuz"},
		{"/", "https://shop.example.com/"},

		{"https://other.example.com/a", "https://other.example.com/a"},
		{"//cdn.example.com/a", "//cdn.example.com/a"},
	}
	for _, c := range cases {
		got := LocalizeMenuURLWith(rule, []string{"en-AU"}, "en-AU", c.raw)
		if got != c.want {
			t.Errorf("LocalizeMenuURLWith(%q) = %q，期望 %q", c.raw, got, c.want)
		}

		if strings.HasPrefix(got, "/") && !strings.HasPrefix(got, "//") {
			t.Errorf("LocalizeMenuURLWith(%q) 产出了站内根相对地址 %q —— 导航链接必须绝对", c.raw, got)
		}
	}
}
