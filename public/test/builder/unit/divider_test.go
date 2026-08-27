package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// dividerDoc 构造 divider 节点文档。
func dividerDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"d1","type":"core.divider","props":`+props+`}]}`)
}

// TestDividerPlain 纯线模式：单层 <hr> 直出 + 线型/宽度三端/对齐 CSS。
func TestDividerPlain(t *testing.T) {
	props := `{"style":"dashed","weight":"2px","color":"var(--color-border)","width":{"desktop":"80%","mobile":"100%"},"align":"center","advanced":{"margin":{"desktop":{"top":"24px","bottom":"24px"}}}}`
	c, err := builder.Compile(dividerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `<hr class="wp-c-d1"/>`) && !strings.Contains(c.HTML, `<hr class="wp-c-d1" />`) {
		t.Errorf("纯线应输出单层 hr: %s", c.HTML)
	}
	for _, want := range []string{
		"border-top: 2px dashed var(--color-border)",
		"width: 80%",
		"margin-left: auto", "margin-right: auto",
		"@media (max-width: 767px)",
		"width: 100%",
		"margin-top: 24px", "margin-bottom: 24px", // Advanced
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestDividerTextInset 嵌入文本：Flex 三段结构 + 文本样式。
func TestDividerTextInset(t *testing.T) {
	props := `{"inset":{"kind":"text","text":"或者","fontSize":"0.75rem","fontWeight":"500","color":"#9ca3af","spacing":"12px","position":"left"}}`
	c, err := builder.Compile(dividerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		`<span class="dt-line"></span>`,
		`<span class="dt-inset dt-inset-text">或者</span>`,
		"display: flex",
		"flex: 0.5", // position left：左短右长
		"flex: 1.5",
		"padding: 0 12px",
		"font-size: 0.75rem", "font-weight: 500", "color: #9ca3af",
	} {
		if !strings.Contains(c.HTML, want) && !strings.Contains(c.CSS, want) {
			t.Errorf("输出缺少 %q", want)
		}
	}
}

// TestDividerIconInset 嵌入图标（内置白名单，svg 内联）。
func TestDividerIconInset(t *testing.T) {
	props := `{"inset":{"kind":"icon","iconName":"star"}}`
	c, err := builder.Compile(dividerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "<svg") || !strings.Contains(c.HTML, `class="dt-inset"`) {
		t.Errorf("图标嵌入异常: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, "<circle") && !strings.Contains(c.HTML, "<path") {
		t.Errorf("star 图标路径缺失: %s", c.HTML)
	}
}

// TestDividerValidate 校验拒绝。
func TestDividerValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"文本嵌入缺文案", `{"inset":{"kind":"text"}}`, "文本嵌入必须提供文案"},
		{"非法内置图标", `{"inset":{"kind":"icon","iconName":"hacker"}}`, "无效的内置图标"},
		{"非法线粗注入", `{"weight":"1px}body{x:1}"}`, "值非法"},
		{"非法颜色注入", `{"color":"red}body{y:1}"}`, "值非法"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(dividerDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// TestDividerDeterministic 确定性构建。
func TestDividerDeterministic(t *testing.T) {
	props := `{"inset":{"kind":"text","text":"OR"},"width":{"desktop":"60%"}}`
	c1, err := builder.Compile(dividerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	c2, err := builder.Compile(dividerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if c1.HTML != c2.HTML || c1.CSS != c2.CSS {
		t.Error("同一文档两次编译输出不一致")
	}
}