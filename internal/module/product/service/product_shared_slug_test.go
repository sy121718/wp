package productservice

// product_shared_slug_test.go — slug 规范化的判据。
//
// 这条链路的输入来自**用户填的文本框**，而结果会被直接拼进 URL（构建期的
// joinArchiveHref：`prefix + slug`）。所以「合法」的定义不是「好看」，而是
// **不能改变 URL 的结构**。

import "testing"

// TestNormalizeSlugRejectsStructureBreakingInput 结构性危险字符必须被挡下来。
//
// 三种实测过的坏输入，各自的后果：
//
//	"A&B"   → "a&b"   `&` 是参数分隔符，品牌页地址被解析成别的东西
//	"a/b"   → "a/b"   多一级路径（/brands/a/b），路由与归档页都对不上
//	"a b"   → 被空格转连字符，这一条是**允许**的（见下面的合法用例）
func TestNormalizeSlugRejectsStructureBreakingInput(t *testing.T) {
	for _, bad := range []string{
		"A&B",   // & 参数分隔符
		"a/b",   // / 路径分隔符
		"a?b",   // ? 查询串起点
		"a#b",   // # 片段标识
		"a=b",   // = 参数赋值
		"a%20b", // % 百分号编码起点
		"a.b",   // 点：扩展名/相对路径语义
		"-abc",  // 首连字符（/brands/-abc）
		"abc-",  // 尾连字符
		"  -  ", // 全空白转连字符后仍是首尾连字符
	} {
		if got := normalizeSlug(bad); got != "" {
			t.Errorf("normalizeSlug(%q) = %q，期望空串（由调用方回退到派生）", bad, got)
		}
	}
}

// TestNormalizeSlugKeepsLegalInput 合法输入只做三项规范化，不擅自改写。
func TestNormalizeSlugKeepsLegalInput(t *testing.T) {
	cases := map[string]string{
		"Nike":          "nike",          // 转小写
		"  Sony  ":      "sony",          // 去首尾空白
		"New Balance":   "new-balance",   // 空格转连字符
		"iPhone-15-Pro": "iphone-15-pro", // 连字符保留
		"a_b":           "a_b",           // 下划线保留
	}
	for in, want := range cases {
		if got := normalizeSlug(in); got != want {
			t.Errorf("normalizeSlug(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestNormalizeSlugAllowsCJK 中文放行。
//
// 为什么不把它也挡掉：这个仓库的站点路径本来就有中文页，强制转拼音会改变既有 URL 的形态；
// 而拼音撞车（"山野" 与 "山月" 同音）时无从判断该给谁加后缀。所以只挡结构性危险字符。
func TestNormalizeSlugAllowsCJK(t *testing.T) {
	for _, in := range []string{"山野", "小米-14", "品牌A"} {
		if got := normalizeSlug(in); got == "" {
			t.Errorf("normalizeSlug(%q) 返回空串，中文应放行", in)
		}
	}
}

// TestDeriveSlugIsStrict deriveSlug 仍是最严的一档（只留 [a-z0-9] 与连字符）。
//
// 它服务的是「从名字自动生成」，来源是任意文本，所以必须保守 —— 与 normalizeSlug
// 的「信任用户」正好相反，两者的宽严差异是有意的。
func TestDeriveSlugIsStrict(t *testing.T) {
	cases := map[string]string{
		"Nike":          "nike",
		"New Balance":   "new-balance",
		"小米 14":         "14", // 中文被丢掉，只剩数字
		"山野":            "",   // 全中文派生不出，由调用方兜底随机码
		"A&B":           "a-b",
		"iPhone 15 Pro": "iphone-15-pro",
	}
	for in, want := range cases {
		if got := deriveSlug(in); got != want {
			t.Errorf("deriveSlug(%q) = %q，期望 %q", in, got, want)
		}
	}
}
