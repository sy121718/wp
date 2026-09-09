package dashboardhttp

// nav_path_alias_test.go — 子页面导航高亮映射（多语言 P5c 翻译工作台）。

import "testing"

// TestNavPathFor 子页面归到所属菜单项；其它路径保持原样（含尾斜杠归一）。
func TestNavPathFor(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"/admin/page/translations", "/admin/pages"},
		{"/admin/page/translations/", "/admin/pages"},
		{"/admin/pages", "/admin/pages"},
		{"/admin/settings", "/admin/settings"},
		{"/admin/settings/", "/admin/settings"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := navPathFor(tc.raw); got != tc.want {
			t.Fatalf("navPathFor(%q) = %q，期望 %q", tc.raw, got, tc.want)
		}
	}
}
