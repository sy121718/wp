package pathkit

// pathkit_test.go — 路径归一化规则的边界（审计 CQ-012）。
//
// 收敛后这里是**唯一**的规则真源：各调用点（pipeline / publication / dashboard）
// 的用例都必须能用本表解释 —— 判断错了不会崩，只会让路由占用判断与访问面产物分裂。

import (
	"strings"
	"testing"
)

// TestNormalizeRoutePath 接受与拒绝两侧都钉住：接受侧给结果字符串，拒绝侧给错误片段。
func TestNormalizeRoutePath(t *testing.T) {
	ok := []struct{ in, want string }{
		{"/", "/"},
		{"/a", "/a"},
		{"/a/", "/a"},
		{"/A", "/A"}, // 大小写不做归一：URL 路径大小写敏感
		{"/中文/路径", "/中文/路径"},
		{"/index", "/"},      // index 归一为根：三者映射同一个文件
		{"/index.html", "/"}, // 同上
		{"/index/", "/"},     // 去尾斜杠之后仍要归一
		{"/a/b/c", "/a/b/c"},
		{"/" + strings.Repeat("x", 499), "/" + strings.Repeat("x", 499)}, // 恰好 500
		{"/a%20b", "/a%20b"}, // 已编码的字符原样保留（不做解码）
		{"/a%22b", "/a%22b"}, // 编码形态的引号同理：只拒裸引号，不拒已正确编码的路径
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
		{"/a\"b", "路径含引号"}, // CQ-012 收敛时漏掉的一档（旧 publication.normalizePath 逐字拒绝了它）
		{"/a'b", "路径含引号"},  // 同上：单引号一并拒绝，两侧实现都是这么写的
		{"/a#frag", "路径含查询串或锚点"},
		{"/a/?x=1", "路径含查询串或锚点"},
		{"/../a", "拒绝路径穿越"},
		{"/a/../b", "拒绝路径穿越"},
		{"/a/./b", "拒绝路径穿越"},
		{"/a/..", "拒绝路径穿越"},
		{"/a/.", "拒绝路径穿越"},
		{"/%2e%2e/x", "拒绝路径穿越"},
		{"/%2E%2E/x", "拒绝路径穿越"}, // 编码大小写都必须拦住
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

// TestMatchKey 匹配键：空、根、多斜杠都收敛到同一个键。
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
