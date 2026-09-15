package unit

// slider_keyboard_fixture_test.go — UI-008 的浏览器走查页：真实产物 + 真实增强脚本，
// 写到 /tmp/slider-a11y.html 供 CDP 发真实按键验证键盘路径（先聚焦容器再按方向键，
// 断言 scrollLeft 确实变化 —— AGENTS.md 明确要求只读属性名不算验证）。
//
// 与 a11y_page_fixture_test.go 同源：组件级断言只能证明标记结构对，
// 证明不了键盘真的走得通。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

const sliderFixtureJSON = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "轮播键盘走查页"}},
  "root": [
    {"id": "sld1", "type": "core.slider", "props": {"perView": {"desktop": 1}, "showArrows": true, "showDots": true}, "children": [
      {"id": "sl1", "type": "core.heading", "props": {"text": "第一屏", "tag": "h3"}},
      {"id": "sl2", "type": "core.heading", "props": {"text": "第二屏", "tag": "h3"}},
      {"id": "sl3", "type": "core.heading", "props": {"text": "第三屏", "tag": "h3"}}
    ]}
  ]
}`

// TestSliderKeyboardFixture 生成走查页并做结构性断言（键盘行为在浏览器里用真实按键验证）。
func TestSliderKeyboardFixture(t *testing.T) {
	page, err := builder.ParsePage([]byte(sliderFixtureJSON))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	compiled, err := compile(t, page)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	for _, want := range []string{
		// 容器可聚焦（方向键路径的入口），且不能是正 tabindex（会打乱全局 Tab 顺序）。
		`data-slider="sld1" tabindex="0"`,
		"sky-slider-track",
		`class="sky-slider-dot"`,
		// 增强脚本内联进产物（键盘处理必须真的送到访客浏览器）。
		"ArrowRight",
		"ArrowLeft",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("走查页缺少 %q", want)
		}
	}
	if strings.Contains(doc, `tabindex="1"`) {
		t.Errorf("产物出现正 tabindex（打乱 Tab 顺序）")
	}

	out := filepath.Join(os.TempDir(), "slider-a11y.html")
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		t.Fatalf("写走查页失败: %v", err)
	}
	t.Logf("走查页已生成：%s（%.1f KB）", out, float64(len(doc))/1024)
}
