package unit

import (
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

// TestNormalizeURLValid 合法路径：规范化尾斜杠、保留根路径。
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
		got, err := pipeline.NormalizeURL(tc.in)
		if err != nil {
			t.Errorf("NormalizeURL(%q) 不应报错: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizeURLInvalid 非法路径：穿越、分隔符、控制字符、保留路径等全部拒绝。
func TestNormalizeURLInvalid(t *testing.T) {
	cases := []string{
		"",
		"about-us",           // 必须以 / 开头
		"/a/../b",            // 路径穿越
		"/a/./b",             // . 段
		"/a//b",              // 重复分隔符
		"/a/%2e%2e/b",        // 编码后穿越
		"/a/%2E/b",           // 编码后穿越（大写）
		"/a\\b",              // 反斜杠
		"/a\x00b",            // 控制字符
		"/a\x1fb",            // 控制字符
		"/admin",             // 保留路径
		"/admin/users",       // 保留路径子路径
		"/api/pages",         // 保留路径
		"/_fragments/header", // 保留路径
		"/assets/logo.svg",   // 保留路径
		"/objects/abc",       // 保留路径
	}
	for _, in := range cases {
		if _, err := pipeline.NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) 应被拒绝", in)
		}
	}
}

// TestNormalizeURLErrorMessage 错误信息可读性。
func TestNormalizeURLErrorMessage(t *testing.T) {
	_, err := pipeline.NormalizeURL("/a/../b")
	if err == nil || !strings.Contains(err.Error(), "路径穿越") {
		t.Errorf("错误信息应指出路径穿越: %v", err)
	}
	_, err = pipeline.NormalizeURL("/admin")
	if err == nil || !strings.Contains(err.Error(), "保留路径") {
		t.Errorf("错误信息应指出保留路径: %v", err)
	}
}
