// jetview_test.go — 组件渲染「旧 Go 字符串拼接」vs「新 Jet 模板」字节等价测试（Phase 0 样板）。
//
// 用同一 Page Document（顶级 container + button + 嵌套 container + button），
// 分别跑 builder.Compile（旧输出）与 nodeViewOf + renderView + templates.NewComponentSet（新输出），
// 断言 HTML 与 CSS 字节完全一致。
package builder

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// jetDocJSON 覆盖：顶级 Section（wp-section）、三端 CSS、入场动效关键帧、
// 形状分隔线（top/bottom）、button 外链/内链/图标前后缀/自定义 class 与 ID/特殊字符转义。
const jetDocJSON = `{
  "settings": {
    "layout": {"mode": "full"},
    "base": {},
    "seo": {"title": "Jet 等价测试", "description": "测试页面"}
  },
  "root": [
    {
      "id": "hero",
      "type": "core.container",
      "props": {
        "tag": "section",
        "layout": {"engine": "flex", "flex": {"direction": "column", "justify": "center", "align": "stretch", "gap": "24px"}},
        "box": {"padding": {"desktop": "40px", "tablet": "24px", "mobile": "16px"}},
        "visual": {"bgColor": "#f7f7f8", "radius": "16px", "shadow": "md"},
        "interaction": {"entrance": "fade-in"},
        "styleEx": {"shapeDivider": "wave"}
      },
      "children": [
        {
          "id": "cta",
          "type": "core.button",
          "props": {
            "text": "立即购买 <span> & 优惠",
            "action": "external",
            "value": "https://example.com/buy?x=1&y=2",
            "target": "blank",
            "rel": "nofollow",
            "size": "lg",
            "shape": "pill",
            "variant": "solid",
            "normal": {"background": "#111111", "color": "#ffffff"},
            "hover": {"background": "#333333"},
            "icon": {"source": "builtin", "name": "arrow-right", "position": "suffix", "spacing": "8px"},
            "advanced": {"customClasses": ["my-cta"], "customId": "cta-btn"}
          }
        },
        {
          "id": "inner",
          "type": "core.container",
          "props": {
            "tag": "div",
            "layout": {"engine": "grid", "grid": {"columns": {"desktop": 2, "tablet": 1}, "columnGap": "16px", "rowGap": "16px"}},
            "box": {"padding": {"desktop": "20px"}},
            "visual": {"borderWidth": "1px", "borderStyle": "solid", "borderColor": "#e0e0e0"},
            "styleEx": {"shapeDivider": "slant", "shapeDividerPosition": "top"}
          },
          "children": [
            {
              "id": "ghost",
              "type": "core.button",
              "props": {
                "text": "了解更多",
                "action": "internal",
                "value": "/about-us",
                "variant": "ghost",
                "icon": {"source": "builtin", "name": "arrow-left", "position": "prefix", "hoverShift": "4px"}
              }
            }
          ]
        }
      ]
    }
  ]
}`

// TestJetViewByteEquivalent 断言新旧渲染路径的 HTML 与 CSS 字节完全一致。
func TestJetViewByteEquivalent(t *testing.T) {
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	// --- 旧路径：builder.Compile ---
	oldRes, err := Compile(page)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if oldRes.HTML == "" || oldRes.CSS == "" {
		t.Fatal("旧输出为空，测试用例无意义（防「空==空」假通过）")
	}

	// --- 新路径：nodeViewOf + Jet 模板 ---
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}

	var b core.CSSBuckets
	compileSettingsCSS(&page.Settings, &b)
	ctx := &core.RenderContext{CSS: &b}

	var htmlBuf strings.Builder
	for _, n := range page.Root {
		v, err := nodeViewOf(n, true, ctx)
		if err != nil {
			t.Fatalf("nodeViewOf: %v", err)
		}
		if err := renderView(v, set, &htmlBuf); err != nil {
			t.Fatalf("renderView(%s): %v", v.Template, err)
		}
	}

	if got, want := htmlBuf.String(), oldRes.HTML; got != want {
		t.Errorf("HTML 字节不一致:\n--- 旧输出 ---\n%s\n--- 新输出 ---\n%s", want, got)
	}
	if got, want := b.String(), oldRes.CSS; got != want {
		t.Errorf("CSS 字节不一致:\n--- 旧输出 ---\n%s\n--- 新输出 ---\n%s", want, got)
	}
}
