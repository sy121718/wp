package unit

import (
	"strings"
	"testing"

	"go_wp/pkg/builder"
)

const validPageJSON = `{
  "settings": {
    "layout": {
      "mode": "boxed",
      "maxWidth": "1200px",
      "safePadding": {"desktop": "16px", "tablet": "12px", "mobile": "8px"}
    },
    "base": {"backgroundColor": "#ffffff", "backgroundImage": "/img/bg.jpg", "backgroundFixed": true},
    "seo": {"title": "测试页", "description": "页面描述"},
    "bodyClasses": ["custom-theme"]
  },
  "root": [
    {
      "id": "hero",
      "type": "core.container",
      "props": {
        "tag": "section",
        "layout": {"engine": "grid", "grid": {"columns": {"desktop": 4, "tablet": 2, "mobile": 1}, "columnGap": "16px", "rowGap": "8px"}},
        "box": {"padding": {"desktop": "40px 24px", "tablet": "24px 16px", "mobile": "16px"}, "minHeight": "200px", "overflow": "hidden"},
        "visual": {"bgGradient": "linear-gradient(to right, #fff, #000)", "borderWidth": "1px", "borderStyle": "solid", "borderColor": "#ddd", "radius": "8px", "shadow": "md"},
        "interaction": {"hoverLift": true, "entrance": "fade-in"}
      },
      "children": [
        {
          "id": "nav-bar",
          "type": "core.container",
          "props": {
            "tag": "nav",
            "layout": {"engine": "flex", "flex": {"direction": "row", "justify": "between", "align": "center", "wrap": true, "gap": "12px"}},
            "interaction": {"sticky": true, "stickyTop": "8px"}
          }
        }
      ]
    }
  ]
}`

func parse(t *testing.T, jsonStr string) *builder.Page {
	t.Helper()
	p, err := builder.ParsePage([]byte(jsonStr))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return p
}

func TestPageSettingsCompile(t *testing.T) {
	c, err := compile(t, parse(t, validPageJSON))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	if c.Title != "测试页" || c.MetaDescription != "页面描述" {
		t.Errorf("SEO 信息丢失: %q / %q", c.Title, c.MetaDescription)
	}

	joined := strings.Join(c.BodyClasses, " ")
	for _, want := range []string{"sky-page", "sky-boxed", "custom-theme"} {
		if !strings.Contains(joined, want) {
			t.Errorf("body class 缺少 %q，实际 %q", want, joined)
		}
	}

	for _, want := range []string{
		"max-width: 1200px",
		"margin-left: auto",
		"padding-left: 16px",
		"@media (max-width: 1024px)",
		"padding-left: 12px",
		"@media (max-width: 767px)",
		"padding-left: 8px",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q", want)
		}
	}

	for _, want := range []string{"background-color: #ffffff", "background-image: url(/img/bg.jpg)", "background-attachment: fixed"} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("body 基底样式缺少 %q", want)
		}
	}
}

func TestContainerHTMLStructure(t *testing.T) {
	c, err := compile(t, parse(t, validPageJSON))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	wantHTML := `<section class="sky-c-hero sky-section"><nav class="sky-c-nav-bar"></nav></section>`
	if c.HTML != wantHTML {
		t.Errorf("HTML 不符合单层语义标签约定:\nwant: %s\ngot:  %s", wantHTML, c.HTML)
	}
	if strings.Contains(c.HTML, "style=") {
		t.Error("HTML 含内联样式，违反编译期静态转译约束")
	}
	if strings.Contains(c.HTML, "<script") {
		t.Error("HTML 含脚本，违反零 JavaScript 布局计算约束")
	}
}

func TestContainerFlexAndGrid(t *testing.T) {
	c, err := compile(t, parse(t, validPageJSON))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{

		"grid-template-columns: repeat(4, 1fr)",
		"repeat(2, 1fr)",
		"repeat(1, 1fr)",
		"column-gap: 16px",
		"row-gap: 8px",

		"display: flex",
		"flex-direction: row",
		"justify-content: space-between",
		"align-items: center",
		"flex-wrap: wrap",
		"gap: 12px",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q", want)
		}
	}
}

func TestContainerBoxVisualInteraction(t *testing.T) {
	c, err := compile(t, parse(t, validPageJSON))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{

		"padding: 40px 24px",
		"padding: 24px 16px",
		"padding: 16px",
		"min-height: 200px",
		"overflow: hidden",

		"background-image: linear-gradient(to right, #fff, #000)",
		"border: 1px solid #ddd",
		"border-radius: 8px",
		"box-shadow: 0 4px 12px rgba(0,0,0,0.12)",

		"position: sticky",
		"top: 8px",

		"transition: transform 0.25s ease, box-shadow 0.25s ease",
		"translateY(-6px)",

		"animation: sky-fade-in 0.6s ease backwards",
		"@keyframes sky-fade-in",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q", want)
		}
	}
}

func TestEntranceDefaultOff(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"s1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}`
	c, err := compile(t, parse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	if strings.Contains(c.CSS, "animation: ") || strings.Contains(c.CSS, "@keyframes") {
		t.Errorf("未配置入场动效却输出了动画:\n%s", c.CSS)
	}
}

func TestDeterministicBuild(t *testing.T) {
	p1 := parse(t, validPageJSON)
	p2 := parse(t, validPageJSON)
	c1, err := compile(t, p1)
	if err != nil {
		t.Fatalf("第一次编译失败: %v", err)
	}
	c2, err := compile(t, p2)
	if err != nil {
		t.Fatalf("第二次编译失败: %v", err)
	}
	if c1.HTML != c2.HTML || c1.CSS != c2.CSS {
		t.Error("同一文档两次编译输出不一致，违反确定性构建约束")
	}
}

func TestRenderDocument(t *testing.T) {
	c, err := compile(t, parse(t, validPageJSON))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := builder.RenderDocument(c)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	for _, want := range []string{
		"<title>测试页</title>",
		`<meta name="description" content="页面描述">`,
		`<body class="sky-page sky-boxed custom-theme">`,
		`<section class="sky-c-hero sky-section">`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("文档缺少 %q", want)
		}
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"非法版心模式", `{"settings":{"layout":{"mode":"diagonal"}},"root":[]}`, "无效的版心模式"},
		{"定宽缺最大宽度", `{"settings":{"layout":{"mode":"boxed"}},"root":[]}`, "最大内容宽度"},
		{"非法语义标签", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"span","layout":{"engine":"flex","flex":{}}}}]}`, "无效的语义标签"},
		{"非法排版引擎", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"table"}}}]}`, "无效的排版引擎"},
		{"flex缺参数", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"flex"}}}]}`, "必须提供 flex 参数"},
		{"栅格列数越界", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"grid","grid":{"columns":{"desktop":13}}}}}]}`, "1~12"},

		{"非法阴影级别", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}},"visual":{"shadow":"xxl"}}}]}`, "无效的阴影级别"},
		{"非法入场动效", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}},"interaction":{"entrance":"spin"}}}]}`, "无效的入场动效"},
		{"节点ID重复", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}},"children":[{"id":"a","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}]}`, "ID 重复"},
		{"未知组件类型", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.weather","props":{}}]}`, "不支持的组件类型"},
		{"非法body class", `{"settings":{"layout":{"mode":"full"},"bodyClasses":["a b"]},"root":[]}`, "无效的 body 自定义 class"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, parse(t, tc.json))
			if err == nil {
				t.Fatalf("期望校验失败，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息不含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

func TestMaliciousCSSInjection(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"引号注入", `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px\"}body{display:none}"}},"root":[]}`},
		{"花括号注入", `{"settings":{"layout":{"mode":"full"},"base":{"backgroundColor":"red}body{x:expr(1)"}},"root":[]}`},
		{"分号注入", `{"settings":{"layout":{"mode":"boxed","maxWidth":"1px;background:url(javascript:1)"}},"root":[]}`},
		{"尖括号注入", `{"settings":{"layout":{"mode":"full"},"base":{"backgroundImage":"<script>alert(1)</script>"}},"root":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, parse(t, tc.json))
			if err == nil {
				t.Error("恶意输入未被拦截")
			}
		})
	}
}

func TestMarginAuto(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"m1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}},"box":{"margin":{"desktop":"0 auto"}}}}]}`
	c, err := compile(t, parse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.CSS, "margin: 0 auto") {
		t.Errorf("margin auto 未生效:\n%s", c.CSS)
	}
}
