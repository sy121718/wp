package pathkit

import (
	"strings"
	"testing"
)

func TestNormalizeRoutePath(t *testing.T) {
	ok := []struct{ in, want string }{
		{"/", "/"},
		{"/a", "/a"},
		{"/a/", "/a"},
		{"/A", "/A"},
		{"/中文/路径", "/中文/路径"},
		{"/index", "/"},
		{"/index.html", "/"},
		{"/index/", "/"},
		{"/a/b/c", "/a/b/c"},
		{"/" + strings.Repeat("x", 499), "/" + strings.Repeat("x", 499)},
		{"/a%20b", "/a%20b"},
		{"/a%22b", "/a%22b"},
	}
	for _, c := range ok {
		got, err := NormalizeRoutePath(c.in)
		if err != nil {
			t.Errorf("NormalizeRoutePath(%q) 返回错误 %v，期望 %q", c.in, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeRoutePath(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}

	bad := []struct{ in, wantErr string }{
		{"", "路径不能为空"},
		{"a", "路径必须以 / 开头"},
		{" /a", "路径必须以 / 开头"},
		{"//", "路径含重复分隔符"},
		{"///", "路径含重复分隔符"},
		{"/a//b", "路径含重复分隔符"},
		{"/a///", "路径含重复分隔符"},
		{"/a ", "路径含空格"},
		{"/a b", "路径含空格"},
		{"/a\\b", "路径含非法字符"},
		{"/a\tb", "路径含控制字符"},
		{"/a?x=1", "路径含查询串或锚点"},
		{"/a\"b", "路径含引号"},
		{"/a'b", "路径含引号"},
		{"/a#frag", "路径含查询串或锚点"},
		{"/a/?x=1", "路径含查询串或锚点"},
		{"/../a", "拒绝路径穿越"},
		{"/a/../b", "拒绝路径穿越"},
		{"/a/./b", "拒绝路径穿越"},
		{"/a/..", "拒绝路径穿越"},
		{"/a/.", "拒绝路径穿越"},
		{"/%2e%2e/x", "拒绝路径穿越"},
		{"/%2E%2E/x", "拒绝路径穿越"},
		{"/%2e/x", "拒绝路径穿越"},
		{"/" + strings.Repeat("x", 500), "路径过长"},
	}
	for _, c := range bad {
		got, err := NormalizeRoutePath(c.in)
		if err == nil {
			t.Errorf("NormalizeRoutePath(%q) = %q，期望被拒绝（%s）", c.in, got, c.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("NormalizeRoutePath(%q) 错误 = %q，期望包含 %q", c.in, err.Error(), c.wantErr)
		}
	}
}

func TestMatchKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/", ""},
		{"///", ""},
		{"  ", ""},
		{"/a", "/a"},
		{"/a/", "/a"},
		{"/a///", "/a"},
		{"  /a/  ", "/a"},
		{"/admin/pages", "/admin/pages"},
		{"/admin/pages/", "/admin/pages"},
	}
	for _, c := range cases {
		if got := MatchKey(c.in); got != c.want {
			t.Errorf("MatchKey(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
