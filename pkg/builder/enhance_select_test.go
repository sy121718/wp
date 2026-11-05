package builder

import (
	"os"
	"strings"
	"testing"
)

func enhanceSrcForTest(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../internal/templates/static/js/enhance.js")
	if err != nil {
		t.Fatalf("读取增强脚本失败: %v", err)
	}
	return string(b)
}

func TestEnhanceScriptForPlainPage(t *testing.T) {
	src := enhanceSrcForTest(t)
	got := enhanceScriptFor(collectHTMLFeatures(`<section><h1>纯内容</h1></section>`), src)
	if got != "" {
		t.Errorf("纯内容页不应输出增强，got %d 字节", len(got))
	}
}

func TestEnhanceScriptForSlidePage(t *testing.T) {
	src := enhanceSrcForTest(t)
	html := "<div class=\"sky-cardstack\" data-cardstack-slide=\"\"><div data-cardstack-track></div></div>"
	got := enhanceScriptFor(collectHTMLFeatures(html), src)
	if !strings.Contains(got, "function initSlideStacks") {
		t.Errorf("slide 页缺少 initSlideStacks")
	}
	for _, fn := range []string{"function initSliders", "function initCardDecks", "function initCounters"} {
		if strings.Contains(got, fn) {
			t.Errorf("slide 页不该注入 %s", fn)
		}
	}

	if !strings.Contains(got, "[initSlideStacks]") {
		t.Errorf("onReady 调用列表应只含 initSlideStacks")
	}
	if strings.Contains(got, "initCardDecks,") {
		t.Errorf("onReady 调用列表残留了未注入的函数（会 ReferenceError）")
	}
}

func TestEnhanceScriptIgnoresSelfReference(t *testing.T) {
	src := enhanceSrcForTest(t)
	html := "<h1>纯内容</h1><script>" + src + "</script>"
	got := enhanceScriptFor(collectHTMLFeatures(html), src)
	if strings.Contains(got, "function initSliders") {
		t.Errorf("script 块内的源码把自己检测出来了，裁剪失效")
	}
}

func TestEnhanceScriptMulti(t *testing.T) {
	src := enhanceSrcForTest(t)
	html := "<div data-slider data-cardstack-deck data-counter></div>"
	got := enhanceScriptFor(collectHTMLFeatures(html), src)
	for _, fn := range []string{"initSliders", "initCardDecks", "initCounters"} {
		if !strings.Contains(got, fn) {
			t.Errorf("多组件页缺少 %s", fn)
		}
	}
	if strings.Contains(got, "function initSlideStacks") {
		t.Errorf("未用到的 slide 不该注入")
	}
}

func TestEnhanceOwnedBlockFromComponent(t *testing.T) {
	src := loadEnhanceSrc(t)
	out := enhanceScriptFor(collectHTMLFeatures(`<div data-counter></div>`), src)
	if !strings.Contains(out, "function initCounters") {
		t.Errorf("命中 data-counter 应内联组件自带的 initCounters")
	}
	if !strings.Contains(out, "initCounters()") {
		t.Errorf("命中 data-counter 应调用 initCounters")
	}

	if strings.Contains(out, "function initSliders") {
		t.Errorf("未命中 data-slider 不该内联 initSliders")
	}
}

func loadEnhanceSrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../internal/templates/static/js/enhance.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenderDocumentSkipsEmptyAndExampleEnhancements(t *testing.T) {
	for _, content := range []string{
		`<h1>纯内容</h1>`,
		`<p>data-slider 与 data-counter 是示例</p>`,
		`<!-- <div data-countdown></div> -->`,
		`<div title="data-lightbox" data-counter-example></div>`,
		`<style>.demo::before{content:'data-slider'}</style>`,
	} {
		out, err := RenderDocument(&CompiledPage{HTML: content, EnhanceSource: enhanceSrcForTest(t)})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "<script") {
			t.Errorf("没有真实增强能力的内容不应输出脚本：%s", content)
		}
	}
}

func TestEnhanceAttributesIgnoreEmbeddedMarkupAndMatchCase(t *testing.T) {
	src := enhanceSrcForTest(t)
	out := enhanceScriptFor(collectHTMLFeatures(`<SCRIPT>const sample='<div data-slider></div>'</SCRIPT><textarea><div data-lightbox></div></textarea><DIV DATA-COUNTER></DIV>`), src)
	if !strings.Contains(out, "function initCounters") {
		t.Fatal("应识别 HTML 大写属性名")
	}
	for _, unwanted := range []string{"function initSliders", "function initLightboxes"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("脚本和原始文本示例不能触发 %s", unwanted)
		}
	}
}
