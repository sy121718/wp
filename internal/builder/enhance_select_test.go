package builder

// enhance_select_test.go — 增强脚本按需拼装。
//
// 背景：enhance.js 约 22KB，8 个互不相关的增强原本全量内联到每个产物。
// 这不是功能问题而是流量问题 —— 纯内容页也要背 22KB，且内联脚本无法缓存。
// 本用例守住「按特征裁剪」与「漏登记特征」两条边界。

import (
	"strings"
	"testing"
)

// TestEnhanceScriptForPlainPage 纯内容页：只注入框架骨架。
func TestEnhanceScriptForPlainPage(t *testing.T) {
	html := "<section class=\"sky-c-h sky-section\"><h1>纯内容</h1></section>"
	got := enhanceScriptFor(html)
	if len(got) > 2000 {
		t.Errorf("纯内容页不该带增强，got %d 字节", len(got))
	}
	for _, fn := range []string{"function initSliders", "function initCounters", "function initSlideStacks"} {
		if strings.Contains(got, fn) {
			t.Errorf("纯内容页不该注入 %s", fn)
		}
	}
	// 骨架必须完整（否则内联脚本会语法错误）
	for _, want := range []string{"(function ()", "onReady(function ()", "})();"} {
		if !strings.Contains(got, want) {
			t.Errorf("骨架不完整，缺少 %q", want)
		}
	}
}

// TestEnhanceScriptForSlidePage 只用 slide：只带 slide 增强。
func TestEnhanceScriptForSlidePage(t *testing.T) {
	html := "<div class=\"sky-cardstack\" data-cardstack-slide=\"\"><div data-cardstack-track></div></div>"
	got := enhanceScriptFor(html)
	if !strings.Contains(got, "function initSlideStacks") {
		t.Errorf("slide 页缺少 initSlideStacks")
	}
	for _, fn := range []string{"function initSliders", "function initCardDecks", "function initCounters"} {
		if strings.Contains(got, fn) {
			t.Errorf("slide 页不该注入 %s", fn)
		}
	}
	// onReady 的调用列表只该引用注入的函数
	if !strings.Contains(got, "[initSlideStacks]") {
		t.Errorf("onReady 调用列表应只含 initSlideStacks")
	}
	if strings.Contains(got, "initCardDecks,") {
		t.Errorf("onReady 调用列表残留了未注入的函数（会 ReferenceError）")
	}
}

// TestEnhanceScriptIgnoresSelfReference 特征扫描必须剥掉 script 块。
// 否则内联的 enhance.js 源码含全部 data-* 字样，会把每个特征都"检测"出来，等于没裁。
func TestEnhanceScriptIgnoresSelfReference(t *testing.T) {
	html := "<h1>纯内容</h1><script>" + enhanceScript + "</script>"
	got := enhanceScriptFor(html)
	if strings.Contains(got, "function initSliders") {
		t.Errorf("script 块内的源码把自己检测出来了，裁剪失效")
	}
}

// TestEnhanceScriptMulti 多个交互组件共存：都要注入。
func TestEnhanceScriptMulti(t *testing.T) {
	html := "<div data-slider data-cardstack-deck data-counter></div>"
	got := enhanceScriptFor(html)
	for _, fn := range []string{"initSliders", "initCardDecks", "initCounters"} {
		if !strings.Contains(got, fn) {
			t.Errorf("多组件页缺少 %s", fn)
		}
	}
	if strings.Contains(got, "function initSlideStacks") {
		t.Errorf("未用到的 slide 不该注入")
	}
}
