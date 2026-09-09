package unit

// lang_test.go — 多语言访问路径映射（多语言 P2，docs/06-D §5 方案 A）。
//
// 覆盖：D1 全语言带前缀、D1 语言根映射 /{lang}/index、语言码白名单、
// 逆运算 StripLangPath、非法输入必须报错（绝不产出半截前缀）。

import (
	"testing"

	"go_wp/internal/pipeline"
)

// TestLangPathMapping 逻辑路径 → 多语言访问路径的映射表。
func TestLangPathMapping(t *testing.T) {
	cases := []struct {
		name string
		lang string
		path string
		want string
	}{
		{"根路径映射 index", "zh-CN", "/", "/zh-CN/index"},
		{"单段路径", "zh-CN", "/about", "/zh-CN/about"},
		{"多段路径", "en-US", "/products/phone", "/en-US/products/phone"},
		{"尾斜杠规范化", "en-US", "/about/", "/en-US/about"},
		{"路径未规范化时补斜杠", "ja", "about", "/ja/about"},
		{"带脚本子标签", "zh-Hans-CN", "/about", "/zh-Hans-CN/about"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pipeline.LangPath(c.lang, c.path)
			if err != nil {
				t.Fatalf("LangPath(%q, %q) 失败: %v", c.lang, c.path, err)
			}
			if got != c.want {
				t.Fatalf("LangPath(%q, %q) = %q，期望 %q", c.lang, c.path, got, c.want)
			}
		})
	}
}

// TestLangPathRejects 非法语言码/路径必须报错（防路径穿越与保留路径污染）。
func TestLangPathRejects(t *testing.T) {
	cases := []struct {
		name string
		lang string
		path string
	}{
		{"空语言", "", "/about"},
		{"空白语言", "   ", "/about"},
		{"语言含斜杠", "zh/CN", "/about"},
		{"语言含点", "..", "/about"},
		{"语言含路径穿越", "../../etc", "/about"},
		{"语言含空格", "zh CN", "/about"},
		{"语言过长", "zh-very-long-language-tag-exceeding-limit", "/about"},
		{"路径穿越", "zh-CN", "/../etc/passwd"},
		{"保留路径", "zh-CN", "/api/login"},
		{"空路径", "zh-CN", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := pipeline.LangPath(c.lang, c.path); err == nil {
				t.Fatalf("LangPath(%q, %q) 应报错", c.lang, c.path)
			}
		})
	}
}

// TestStripLangPath 逆运算：/{lang}/path → 逻辑路径。
func TestStripLangPath(t *testing.T) {
	cases := []struct{ lang, path, want string }{
		{"zh-CN", "/zh-CN/index", "/"},
		{"zh-CN", "/zh-CN", "/"},
		{"zh-CN", "/zh-CN/about", "/about"},
		{"zh-CN", "/zh-CN/a/b", "/a/b"},
		{"zh-CN", "/about", "/about"},
		{"zh-CN", "/en-US/about", "/en-US/about"},
	}
	for _, c := range cases {
		if got := pipeline.StripLangPath(c.lang, c.path); got != c.want {
			t.Errorf("StripLangPath(%q, %q) = %q，期望 %q", c.lang, c.path, got, c.want)
		}
	}
}

// TestNormalizeLang 语言码规范化。
func TestNormalizeLang(t *testing.T) {
	if got, err := pipeline.NormalizeLang(" en-US "); err != nil || got != "en-US" {
		t.Fatalf("NormalizeLang 应去空白并保留原大小写: %q %v", got, err)
	}
	if _, err := pipeline.NormalizeLang(""); err == nil {
		t.Fatal("空语言码应报错")
	}
}
