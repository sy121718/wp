package unit

import (
	publish "go_wp/pkg/publish"
	"strings"
	"testing"
)

func TestNormalizeURLValid(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/", "/"},
		{"/about-us", "/about-us"},
		{"/about-us/", "/about-us"},
		{"/products/phone", "/products/phone"},
		{"/promo/2026/summer", "/promo/2026/summer"},
		{"/aB-c_1", "/aB-c_1"},
	}
	for _, tc := range cases {
		got, err := publish.NormalizeURL(tc.in)
		if err != nil {
			t.Errorf("NormalizeURL(%q) 不应报错: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeURLInvalid(t *testing.T) {
	cases := []string{
		"",
		"about-us",
		"/a/../b",
		"/a/./b",
		"/a//b",
		"/a/%2e%2e/b",
		"/a/%2E/b",
		"/a\\b",
		"/a b",
		"/a b/c",
		"/a\x00b",
		"/a\x1fb",
		"/admin",
		"/admin/users",
		"/api/pages",
		"/_fragments/header",
		"/assets/logo.svg",
		"/objects/abc",
	}
	for _, in := range cases {
		if _, err := publish.NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) 应被拒绝", in)
		}
	}
}

func TestNormalizeURLLengthBoundary(t *testing.T) {
	ok := "/" + strings.Repeat("a", 499)
	if got, err := publish.NormalizeURL(ok); err != nil || got != ok {
		t.Errorf("500 字符路径应接受: got=%q err=%v", got, err)
	}
	tooLong := "/" + strings.Repeat("a", 500)
	if _, err := publish.NormalizeURL(tooLong); err == nil {
		t.Errorf("501 字符路径应被拒绝")
	}
}

func TestNormalizeURLErrorMessage(t *testing.T) {
	_, err := publish.NormalizeURL("/a/../b")
	if err == nil || !strings.Contains(err.Error(), "路径穿越") {
		t.Errorf("错误信息应指出路径穿越: %v", err)
	}
	_, err = publish.NormalizeURL("/admin")
	if err == nil || !strings.Contains(err.Error(), "保留路径") {
		t.Errorf("错误信息应指出保留路径: %v", err)
	}
}
