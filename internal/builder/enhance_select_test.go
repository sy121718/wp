package builder

// enhance_select_test.go — 增强脚本按需拼装。
//
// 背景：enhance.js 约 22KB，8 个互不相关的增强原本全量内联到每个产物。
// 这不是功能问题而是流量问题 —— 纯内容页也要背 22KB，且内联脚本无法缓存。
// 本用例守住「按特征裁剪」与「漏登记特征」两条边界。

import (
	"os"
	"strings"
	"testing"
)

// enhanceSrcForTest 读取增强脚本源码。
//
// 源码已挪到 internal/templates/static/js/（运行时资产目录，构建期由装配层注入给
// builder）；builder 不依赖 templates 包，测试里按仓库相对路径读真源码 ——
// 这样测的是「真实文件能否被正确裁剪」，而不是随手造的一段假源码。
func enhanceSrcForTest(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../templates/static/js/enhance.js")
	if err != nil {
		t.Fatalf("读取增强脚本失败: %v", err)
	}
	return string(b)
}

// TestEnhanceScriptForPlainPage 纯内容页：只注入框架骨架。
func TestEnhanceScriptForPlainPage(t *testing.T) {
	src := enhanceSrcForTest(t)
	html := "<section class=\"sky-c-h sky-section\"><h1>纯内容</h1></section>"
	got := enhanceScriptFor(html, src)
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
	src := enhanceSrcForTest(t)
	html := "<div class=\"sky-cardstack\" data-cardstack-slide=\"\"><div data-cardstack-track></div></div>"
	got := enhanceScriptFor(html, src)
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
	src := enhanceSrcForTest(t)
	html := "<h1>纯内容</h1><script>" + src + "</script>"
	got := enhanceScriptFor(html, src)
	if strings.Contains(got, "function initSliders") {
		t.Errorf("script 块内的源码把自己检测出来了，裁剪失效")
	}
}

// TestEnhanceScriptMulti 多个交互组件共存：都要注入。
func TestEnhanceScriptMulti(t *testing.T) {
	src := enhanceSrcForTest(t)
	html := "<div data-slider data-cardstack-deck data-counter></div>"
	got := enhanceScriptFor(html, src)
	for _, fn := range []string{"initSliders", "initCardDecks", "initCounters"} {
		if !strings.Contains(got, fn) {
			t.Errorf("多组件页缺少 %s", fn)
		}
	}
	if strings.Contains(got, "function initSlideStacks") {
		t.Errorf("未用到的 slide 不该注入")
	}
}

// TestEnhanceOwnedBlockFromComponent 组件自带的增强块（就近放置）能正确内联。
//
// counter 的行为块已从 enhance.js 迁到 components/counter/enhance.js，经 core.RegisterEnhanceBlock
// 注册。这条路径在此前不存在，所以单独钉住：注册没生效的话，命中 data-counter 的页面
// 会静默失去「数字递增」交互 —— 页面照常渲染，只是数字不动，很难归因。
func TestEnhanceOwnedBlockFromComponent(t *testing.T) {
	src := loadEnhanceSrc(t)
	out := enhanceScriptFor(`<div data-counter></div>`, src)
	if !strings.Contains(out, "function initCounters") {
		t.Errorf("命中 data-counter 应内联组件自带的 initCounters")
	}
	if !strings.Contains(out, "initCounters()") {
		t.Errorf("命中 data-counter 应调用 initCounters")
	}
	// 组件自带的块不该影响存量段落的挑选。
	if strings.Contains(out, "function initSliders") {
		t.Errorf("未命中 data-slider 不该内联 initSliders")
	}
}

// loadEnhanceSrc 读存量增强源码（enhance.js 现只剩框架 + 未迁移的段落）。
func loadEnhanceSrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../templates/static/js/enhance.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
