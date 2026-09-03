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

// --- Phase 1 叶子组件字节等价测试 ---

// assertJetByteEquivalent 用同一组 root 节点（JSON）分别跑旧 Compile 与新 nodeViewOf+renderView，
// 断言 HTML 与 CSS 字节完全一致。
func assertJetByteEquivalent(t *testing.T, rootJSON string) {
	t.Helper()
	doc := `{"settings": {"layout": {"mode": "full"}, "seo": {"title": "等价测试"}}, "root": ` + rootJSON + `}`
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	oldRes, err := Compile(page)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if oldRes.HTML == "" {
		t.Fatal("旧输出 HTML 为空，测试用例无意义（防「空==空」假通过）")
	}

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

// TestJetViewHeadingByteEquivalent 标题组件：副标题/高亮盒/字重/装饰/自定义 class 与 ID/特殊字符转义。
func TestJetViewHeadingByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"h1","type":"core.heading","props":{"text":"标题 <b>&</b>","tag":"h2","subtitle":"副标题 & 更多","highlightColor":"#fff000","highlightPadding":"4px 8px","highlightRadius":"6px","advanced":{"customClasses":["my-cls"],"customId":"my-heading"}}},
		{"id":"h2","type":"core.heading","props":{"text":"二级","tag":"h3","weight":"bold","transform":"uppercase","decor":{"decoration":"underline","decorationColor":"#f00"},"color":"#333"}}
	]`)
}

// TestJetViewTextByteEquivalent 正文组件：纯文本转义 / 富文本 sanitize。
func TestJetViewTextByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"t1","type":"core.text","props":{"mode":"plaintext","plainTag":"p","text":"纯文本 <b>&</b> 特殊"}},
		{"id":"t2","type":"core.text","props":{"mode":"richtext","text":"<p>富文本 <strong>加粗</strong> & 更多</p><script>alert(1)</script>"}}
	]`)
}

// TestJetViewImageByteEquivalent 图片组件：懒加载/立即加载、链接/灯箱/图注、customId 分支。
func TestJetViewImageByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"img1","type":"core.image","props":{"src":"https://example.com/a.jpg","alt":"图片 & 说明","loading":"eager","fetchPriority":"high"}},
		{"id":"img2","type":"core.image","props":{"src":"https://example.com/b.jpg","alt":"链接图","clickAction":"link","link":"https://example.com/t?x=1&y=2","linkTarget":"blank","linkRel":"nofollow","advanced":{"customId":"img-link"}}},
		{"id":"img3","type":"core.image","props":{"src":"https://example.com/c.jpg","alt":"灯箱","clickAction":"lightbox","caption":"图注 & 内容"}}
	]`)
}

// TestJetViewDividerByteEquivalent 分割线组件：纯线 / 文本嵌入 / 图标嵌入 / 自定义 ID。
func TestJetViewDividerByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"d1","type":"core.divider","props":{"style":"solid","weight":"2px","color":"#ccc"}},
		{"id":"d2","type":"core.divider","props":{"inset":{"kind":"text","text":"或者 & 更多","position":"left","spacing":"8px","fontSize":"14px"}}},
		{"id":"d3","type":"core.divider","props":{"inset":{"kind":"icon","iconName":"star","position":"right"},"advanced":{"customClasses":["x"],"customId":"div-id"}}}
	]`)
}

// TestJetViewSpacerByteEquivalent 间隔组件：三端高度 + Advanced 外边距。
func TestJetViewSpacerByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"s1","type":"core.spacer","props":{"height":{"desktop":"80px","tablet":"40px","mobile":"20px"},"advanced":{"margin":{"desktop":{"top":"10px","bottom":"20px"}}}}}
	]`)
}

// TestJetViewListByteEquivalent 列表组件：图标/序号/圆点三种样式 + repeater items + 链接。
func TestJetViewListByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"l1","type":"core.list","props":{"style":"icon","items":[{"icon":"check","text":"第一项 & 特殊","link":"https://example.com/1"},{"icon":"star","text":"第二项"},{"text":"无图标项"}]}},
		{"id":"l2","type":"core.list","props":{"style":"number","items":[{"text":"甲"},{"text":"乙"}]}},
		{"id":"l3","type":"core.list","props":{"style":"dot","items":[{"text":"点1"},{"text":"点2"}]}}
	]`)
}

// TestJetViewInfoboxByteEquivalent 信息框组件：图标/媒体图 + 按钮化/纯链接/无链接三分支。
func TestJetViewInfoboxByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"ib1","type":"core.infobox","props":{"icon":"check","title":"标题 & 内容","text":"描述","subtitle":"副标题","link":"https://example.com","btnText":"了解更多"}},
		{"id":"ib2","type":"core.infobox","props":{"mediaImage":"https://example.com/m.jpg","title":"媒体图","text":"文本"}},
		{"id":"ib3","type":"core.infobox","props":{"icon":"star","title":"链接卡","link":"https://example.com/2"}}
	]`)
}

// TestJetViewSocialbuttonsByteEquivalent 社交按钮组件：品牌色有序 / 自定义色 + 多平台 repeater。
func TestJetViewSocialbuttonsByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"sb1","type":"core.social_buttons","props":{"color":"brand","items":[{"platform":"facebook","url":"https://facebook.com/x"},{"platform":"x","url":"https://x.com/y"}]}},
		{"id":"sb2","type":"core.social_buttons","props":{"color":"custom","customColor":"#123456","items":[{"platform":"youtube","url":"https://youtube.com/z"}]}}
	]`)
}

// TestJetViewVideoByteEquivalent 视频组件：iframe 嵌入 / 本地 video 双形态。
func TestJetViewVideoByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"v1","type":"core.video","props":{"url":"https://www.youtube.com/watch?v=abc12345","ratio":"16:9"}},
		{"id":"v2","type":"core.video","props":{"url":"https://example.com/storage/v.mp4","poster":"https://example.com/p.jpg","controls":true,"muted":true,"loop":true,"preload":"auto","ratio":"4:3","fullWidth":true}}
	]`)
}

// TestJetViewCounterByteEquivalent 计数器组件：数据属性 + 前缀/后缀/标签。
func TestJetViewCounterByteEquivalent(t *testing.T) {
	assertJetByteEquivalent(t, `[
		{"id":"c1","type":"core.counter","props":{"start":0,"end":12345,"decimals":2,"prefix":"$","suffix":"+","label":"满意客户","duration":3,"color":"#111"}},
		{"id":"c2","type":"core.counter","props":{"end":99,"label":"百分比","suffix":"%"}}
	]`)
}
