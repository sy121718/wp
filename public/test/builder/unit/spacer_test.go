package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// spacerDoc 构造 spacer 节点文档。
func spacerDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sp1","type":"core.spacer","props":`+props+`}]}`)
}

// TestSpacerCompile 基座组件全链路：三端高度 CSS + 单层 div + Advanced 织入。
func TestSpacerCompile(t *testing.T) {
	props := `{"height":{"desktop":"80px","tablet":"48px","mobile":"24px"},"advanced":{"margin":{"desktop":{"bottom":"16px"}},"customClasses":["gap-fix"],"customId":"anchor-gap"}}`
	c, err := builder.Compile(spacerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	// HTML：单层 div，class 合并（节点类 + Advanced 自定义类），customId 注入。
	wantHTML := `<div class="wp-c-sp1 gap-fix" id="anchor-gap"></div>`
	if c.HTML != wantHTML {
		t.Errorf("HTML 异常:\nwant %s\ngot  %s", wantHTML, c.HTML)
	}

	// CSS：三端高度 + Advanced margin。
	for _, want := range []string{
		"height: 80px",
		"@media (max-width: 1024px)",
		"height: 48px",
		"@media (max-width: 767px)",
		"height: 24px",
		"margin-bottom: 16px", // Advanced 由基座编译
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestSpacerValidate 基座校验管线：叶子约束 / ID 查重 / 高度白名单 / Advanced 规则。
func TestSpacerValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"高度注入", `{"height":{"desktop":"1px}body{x:1}"}}`, "无效的 desktop 端高度"},
		{"Advanced 非法 class", `{"advanced":{"customClasses":["wp-evil"]}}`, "wp- 保留前缀"},
		{"Advanced 非法透明度", `{"advanced":{"opacity":150}}`, "0~100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(spacerDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}

	// 叶子约束：children 在节点层。
	t.Run("叶子约束", func(t *testing.T) {
		doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sp1","type":"core.spacer","props":{},"children":[{"id":"c1","type":"core.spacer","props":{}}]}]}`
		_, err := builder.Compile(mustParse(t, doc))
		if err == nil || !strings.Contains(err.Error(), "叶子节点") {
			t.Errorf("期望叶子节点错误，实际: %v", err)
		}
	})

	// ID 查重（基座 ValidateNodeID）。
	t.Run("ID查重", func(t *testing.T) {
		doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sp1","type":"core.spacer","props":{}},{"id":"sp1","type":"core.spacer","props":{}}]}`
		_, err := builder.Compile(mustParse(t, doc))
		if err == nil || !strings.Contains(err.Error(), "ID 重复") {
			t.Errorf("期望 ID 重复错误，实际: %v", err)
		}
	})
}

// TestSpacerDeterministic 确定性构建。
func TestSpacerDeterministic(t *testing.T) {
	props := `{"height":{"desktop":"40px"}}`
	c1, err := builder.Compile(spacerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	c2, err := builder.Compile(spacerDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if c1.HTML != c2.HTML || c1.CSS != c2.CSS {
		t.Error("同一文档两次编译输出不一致")
	}
}
