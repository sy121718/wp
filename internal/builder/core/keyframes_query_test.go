package core

import (
	"strings"
	"testing"
)

// TestKeyframeCSSByName 后台按需内联动效的取用逻辑。
//
// 后台没有 props 管线，但也不该常驻加载 63 条营销动效 ——
// 页面声明几个就内联几个。这里钉住「空声明零字节」与「未知名字不炸」。
func TestKeyframeCSSByName(t *testing.T) {
	if got := KeyframeCSS(nil); got != "" {
		t.Errorf("空声明应当零字节，得到 %q", got)
	}
	if got := KeyframeCSS([]string{"", "   "}); got != "" {
		t.Errorf("空白声明应当零字节，得到 %q", got)
	}

	one := KeyframeCSS([]string{"sky-fade-up"})
	if !strings.Contains(one, "@keyframes sky-fade-up") {
		t.Errorf("未包含请求的动效: %q", one)
	}
	// 只该有请求的那一条 —— 这正是「按需」的含义。
	if strings.Count(one, "@keyframes ") != 1 {
		t.Errorf("应只内联 1 条，得到 %d 条", strings.Count(one, "@keyframes "))
	}

	// 去重：同一名字写两遍只出现一次。
	dup := KeyframeCSS([]string{"sky-fade-up", "sky-fade-up"})
	if strings.Count(dup, "@keyframes ") != 1 {
		t.Errorf("重复声明应只内联一次，得到 %d 条", strings.Count(dup, "@keyframes "))
	}

	// 未知名字跳过并留痕，不能影响已知的那些（后台页面是开发者写的，写错属笔误）。
	mixed := KeyframeCSS([]string{"sky-fade-up", "sky-not-a-real-name", "sky-fade-in-top-left"})
	if !strings.Contains(mixed, "@keyframes sky-fade-up") || !strings.Contains(mixed, "@keyframes sky-fade-in-top-left") {
		t.Errorf("已知动效被未知名字连累: %q", mixed)
	}
	if strings.Contains(mixed, "sky-not-a-real-name") {
		t.Errorf("未知名字不该出现在产物里: %q", mixed)
	}
	if strings.Count(mixed, "@keyframes ") != 2 {
		t.Errorf("应只有 2 条已知动效，得到 %d 条", strings.Count(mixed, "@keyframes "))
	}
}

// TestKeyframeNamesAvailable 词汇表非空且与 catalog 同源。
func TestKeyframeNamesAvailable(t *testing.T) {
	names := KeyframeNames()
	if len(names) != len(keyframesCatalog) {
		t.Errorf("名字数 %d 与 catalog %d 不一致", len(names), len(keyframesCatalog))
	}
	for _, want := range []string{"sky-fade-up", "sky-loop-flash"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("词汇表缺少 %s", want)
		}
	}
}
