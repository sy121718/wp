package form

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestFormCSSParsesAndKeepsRules(t *testing.T) {
	var b core.CSSBuckets
	const scope = ".sky-node-test"
	if err := core.ApplyComponentCSS(&b, scope, formCSS); err != nil {
		t.Fatalf("解析 form.css 失败: %v", err)
	}
	out := b.String()

	cases := []struct {
		desc string
		want []string
	}{
		{"容器纵向布局", []string{".sky-node-test {", "display: flex", "flex-direction: column", "gap: var(--sky-space-7, 16px)"}},
		{"字段容器", []string{".sky-node-test .sky-form-field {", "gap: var(--sky-space-3, 6px)"}},
		{"标签", []string{".sky-node-test .sky-form-field label {", "font-size: var(--sky-fs-md, 14px)", "font-weight: 600"}},
		{"checkbox 标签", []string{".sky-node-test .sky-form-field label.sky-form-check {", "display: inline-flex", "font-weight: 400"}},
		{"checkbox 输入", []string{".sky-node-test .sky-form-field label.sky-form-check input {", "width: auto"}},
		{"输入控件 :is() 合并", []string{".sky-node-test :is(input[type=text],input[type=email],select) {", "padding: var(--sky-space-5, 10px) var(--sky-space-6, 12px)", "border-radius: var(--sky-radius-md, 6px)"}},
		{"textarea", []string{".sky-node-test textarea {", "min-height: 96px", "resize: vertical"}},
		{"聚焦 ring（占位指令已展开）", []string{".sky-node-test :is(input,textarea,select):focus {"}},
		{"校验错误态 父容器", []string{".sky-node-test .sky-form-field:has(:user-invalid) {", "color: var(--sky-c-danger, #dc2626)"}},
		{"校验错误态 输入框", []string{".sky-node-test :user-invalid {", "box-shadow: 0 0 0 3px rgba(220, 38, 38, .12)"}},
		{"提交按钮", []string{".sky-node-test .sky-form-submit {", "padding: 11px var(--sky-space-8, 24px)", "cursor: pointer"}},
		{"提交按钮 hover", []string{".sky-node-test .sky-form-submit:hover {", "var(--sky-btn-hover-bg"}},
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

func TestFormCSSRejectsBadSource(t *testing.T) {
	bad := []struct {
		name string
		src  string
	}{
		{"选择器缺 &", "div { color: red; }"},
		{"声明缺冒号", "& { colorred }"},
		{"自造断点", "@media (max-width: 500px) { & { color: red; } }"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			var b core.CSSBuckets
			if err := core.ApplyComponentCSS(&b, ".x", c.src); err == nil {
				t.Errorf("应当报错但通过了: %s", c.src)
			}
		})
	}
}
