package unit

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

		`data-slider="sld1" tabindex="0"`,
		"sky-slider-track",
		`class="sky-slider-dot"`,

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
