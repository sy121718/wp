package checkoutform

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestCheckoutFormCSSParsesAndKeepsRules 逐条核对关键规则仍在。
//
// 样式源从 Go 字符串迁到 .css 后多了一层解析：解析器写错、选择器替换错、占位指令
// 没展开，任何一处出错都会让产物「悄悄少了样式」，页面上只表现为「有点不对劲」。
func TestCheckoutFormCSSParsesAndKeepsRules(t *testing.T) {
	var b core.CSSBuckets
	const scope = ".sky-c-test"
	if err := core.ApplyComponentCSS(&b, scope, checkoutformCSS); err != nil {
		t.Fatalf("解析 checkoutform.css 失败: %v", err)
	}
	out := b.String()
	cases := []struct {
		desc string
		want []string
	}{
		{"容器纵向布局", []string{".sky-c-test {", "display: flex", "flex-direction: column", "gap: 20px"}},
		{"分组清掉 fieldset 默认样式", []string{".sky-c-test .sky-checkout-group {", "border: 0", "min-width: 0"}},
		{"字段栅格用 auto-fit + min(100%, …)", []string{".sky-c-test .sky-checkout-grid {", "grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr))"}},
		{"整行字段跨列", []string{".sky-c-test .sky-checkout-field.is-wide {", "grid-column: 1 / -1"}},
		{"输入控件 :is() 合并", []string{".sky-c-test :is(input[type=text],input[type=email],input[type=tel],select) {", "min-height: 44px", "border-radius: 6px"}},
		{"textarea", []string{".sky-c-test textarea {", "min-height: 96px", "resize: vertical"}},
		{"聚焦 ring（占位指令已展开）", []string{".sky-c-test :is(input,textarea,select):focus {"}},
		{"校验错误态 父容器", []string{".sky-c-test .sky-checkout-field:has(:user-invalid) {", "var(--sky-c-danger, #dc2626)"}},
		{"提交按钮", []string{".sky-c-test .sky-checkout-submit {", "padding: 12px 28px", "cursor: pointer"}},
		{"提交按钮 hover", []string{".sky-c-test .sky-checkout-submit:hover {", "var(--sky-btn-hover-bg"}},
	}
	for _, c := range cases {
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s：产物 CSS 里找不到 %q", c.desc, w)
			}
		}
	}
	if strings.Contains(out, "@focus-") {
		t.Errorf("占位指令没有展开，产物里残留: %s", out)
	}
	if strings.Contains(out, "&") {
		t.Errorf("产物里残留未替换的 & : %s", out)
	}
}

// TestCheckoutFormCSSTouchEquivalence 触屏等价形态：按压反馈必须存在（触屏上没有 hover）。
func TestCheckoutFormCSSTouchEquivalence(t *testing.T) {
	var b core.CSSBuckets
	const scope = ".sky-c-test"
	if err := core.ApplyComponentCSS(&b, scope, checkoutformCSS); err != nil {
		t.Fatalf("解析 checkoutform.css 失败: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, ".sky-c-test .sky-checkout-submit:active") {
		t.Errorf("缺触屏按压反馈（@active 桶）:\n%s", out)
	}
}
