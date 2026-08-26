package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// mapResolver 简单字段映射 ContentResolver 测试实现。
type mapResolver map[string]string

func (m mapResolver) ResolveString(field string) (string, error) {
	return m[field], nil
}

// headingDoc 构造 heading 节点文档。
func headingDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":`+props+`}]}`)
}

// TestHeadingStaticCompile 静态文本：语义标签 + 排版全量编译 + 单层无包装。
func TestHeadingStaticCompile(t *testing.T) {
	props := `{
		"text": "核心产品特性介绍",
		"tag": "h2",
		"typography": {
			"desktop": {"fontSize": "2rem", "lineHeight": "1.2", "textAlign": "center"},
			"tablet": {"fontSize": "1.75rem"},
			"mobile": {"fontSize": "clamp(1.25rem, 4vw, 1.5rem)", "textAlign": "left"}
		},
		"weight": "semibold",
		"letterSpacing": "0.05em",
		"transform": "uppercase",
		"decor": {"decoration": "underline", "decorationColor": "#f00"},
		"color": "var(--color-primary)",
		"lineClamp": 2,
		"textShadow": "subtle",
		"advanced": {"margin": {"desktop": {"bottom": "24px"}}, "customClasses": ["custom-heading"]}
	}`
	c, err := builder.Compile(headingDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	wantHTML := `<h2 class="wp-c-h1 custom-heading">核心产品特性介绍</h2>`
	if c.HTML != wantHTML {
		t.Errorf("HTML 不符合单层语义标签:\nwant %s\ngot  %s", wantHTML, c.HTML)
	}

	for _, want := range []string{
		"font-size: 2rem",
		"line-height: 1.2",
		"text-align: center",
		"@media (max-width: 1024px)",
		"font-size: 1.75rem",
		"@media (max-width: 767px)",
		"font-size: clamp(1.25rem, 4vw, 1.5rem)",
		"font-weight: 600", // semibold token
		"letter-spacing: 0.05em",
		"text-transform: uppercase",
		"text-decoration: underline #f00",
		"color: var(--color-primary)",
		"-webkit-line-clamp: 2",
		"text-shadow: 0 1px 2px rgba(0,0,0,0.45)",
		"margin-bottom: 24px", // advanced：部分设置输出长属性
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestHeadingBindingCompile CMS 绑定：发布期静态填入 + 空值 Fallback 兜底。
func TestHeadingBindingCompile(t *testing.T) {
	resolver := mapResolver{"post.title": "2026 年度旗舰款无线降噪耳机"}
	props := `{"binding":{"field":"post.title","fallback":"默认标题"},"tag":"h1"}`

	c, err := builder.Compile(headingDoc(t, props), builder.WithContentResolver(resolver))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, ">2026 年度旗舰款无线降噪耳机</h1>") {
		t.Errorf("绑定值未静态填入: %s", c.HTML)
	}

	// 字段为空 → Fallback。
	empty := mapResolver{"post.title": ""}
	c2, err := builder.Compile(headingDoc(t, props), builder.WithContentResolver(empty))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c2.HTML, ">默认标题</h1>") {
		t.Errorf("Fallback 未生效: %s", c2.HTML)
	}
}

// TestHeadingBindingMissingResolver 缺内容解析器报明确错误。
func TestHeadingBindingMissingResolver(t *testing.T) {
	props := `{"binding":{"field":"post.title"}}`
	_, err := builder.Compile(headingDoc(t, props))
	if err == nil || !strings.Contains(err.Error(), "内容解析器") {
		t.Errorf("缺少解析器应报错: %v", err)
	}
}

// TestHeadingXSS 文本内容转义。
func TestHeadingXSS(t *testing.T) {
	props := `{"text":"<script>alert(1)</script>","tag":"h3"}`
	c, err := builder.Compile(headingDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c.HTML, "<script>") {
		t.Errorf("文本未转义: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, "&lt;script&gt;") {
		t.Errorf("转义输出异常: %s", c.HTML)
	}
}

// TestHeadingValidateErrors 校验拒绝。
func TestHeadingValidateErrors(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"无内容", `{"tag":"h2"}`, "必须提供静态文本或 CMS 绑定"},
		{"非法标签", `{"text":"x","tag":"p"}`, "无效的语义标签"},
		{"非法字号", `{"text":"x","typography":{"desktop":{"fontSize":"2url"}}}`, "无效的"},
		{"非法对齐", `{"text":"x","typography":{"desktop":{"textAlign":"middle"}}}`, "无效的"},
		{"非法字重", `{"text":"x","weight":"bolder"}`, "无效的字重"},
		{"非法转换", `{"text":"x","transform":"titlecase"}`, "无效的文字转换"},
		{"非法装饰", `{"text":"x","decor":{"decoration":"blink"}}`, "无效的文本装饰"},
		{"非法颜色", `{"text":"x","color":"red}body{y:1}"}`, "无效的文字颜色"},
		{"截断越界", `{"text":"x","lineClamp":9}`, "1~6"},
		{"非法文字阴影", `{"text":"x","textShadow":"glow"}`, "无效的文字阴影"},
		{"非法绑定路径", `{"binding":{"field":"Post.Title"}}`, "无效的绑定字段路径"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(headingDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}

	// 带子节点：children 在节点层（与 props 平级），叶子组件应拒绝。
	t.Run("带子节点", func(t *testing.T) {
		doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"x"},"children":[{"id":"c1","type":"core.heading","props":{"text":"y"}}]}]}`
		_, err := builder.Compile(mustParse(t, doc))
		if err == nil || !strings.Contains(err.Error(), "叶子节点") {
			t.Errorf("期望叶子节点错误，实际: %v", err)
		}
	})
}

// TestHeadingWeightNumeric 字重直接数值 100~900。
func TestHeadingWeightNumeric(t *testing.T) {
	c, err := builder.Compile(headingDoc(t, `{"text":"x","weight":"800"}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.CSS, "font-weight: 800") {
		t.Errorf("数值字重未生效:\n%s", c.CSS)
	}
}

// TestHeadingDefaultTag 默认标签 h2。
func TestHeadingDefaultTag(t *testing.T) {
	c, err := builder.Compile(headingDoc(t, `{"text":"标题"}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.HasPrefix(c.HTML, "<h2") {
		t.Errorf("默认标签应为 h2: %s", c.HTML)
	}
}