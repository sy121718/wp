package form

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestFormCSSParsesAndKeepsRules 迁移到 form.css 后，逐条核对关键规则仍在。
//
// 背景：样式从 compileCSS 的 12 处 b.Add 迁成 CSS 文件（form.css），中间多了一层解析。
// 解析器写错、选择器替换错、占位指令没展开 —— 任何一处出错都会让产物「悄悄少了样式」，
// 而这类缺陷在页面上往往只表现为「有点不对劲」，很难追。所以这里把每条规则都钉住。
func TestFormCSSParsesAndKeepsRules(t *testing.T) {
	var b core.CSSBuckets
	const scope = ".sky-node-test"
	if err := core.ApplyComponentCSS(&b, scope, formCSS); err != nil {
		t.Fatalf("解析 form.css 失败: %v", err)
	}
	out := b.String()

	// 每条选择器 + 每条只属于它的声明，逐一对齐迁移前 compileCSS 的行为。
	cases := []struct {
		desc string
		want []string
	}{
		{"容器纵向布局", []string{".sky-node-test {", "display: flex", "flex-direction: column", "gap: 16px"}},
		{"字段容器", []string{".sky-node-test .sky-form-field {", "gap: 6px"}},
		{"标签", []string{".sky-node-test .sky-form-field label {", "font-size: 14px", "font-weight: 600"}},
		{"checkbox 标签", []string{".sky-node-test .sky-form-field label.sky-form-check {", "display: inline-flex", "font-weight: 400"}},
		{"checkbox 输入", []string{".sky-node-test .sky-form-field label.sky-form-check input {", "width: auto"}},
		{"输入控件 :is() 合并", []string{".sky-node-test :is(input[type=text],input[type=email],select) {", "padding: 10px 12px", "border-radius: 6px"}},
		{"textarea", []string{".sky-node-test textarea {", "min-height: 96px", "resize: vertical"}},
		{"聚焦 ring（占位指令已展开）", []string{".sky-node-test :is(input,textarea,select):focus {"}},
		{"校验错误态 父容器", []string{".sky-node-test .sky-form-field:has(:user-invalid) {", "color: var(--sky-c-danger, #dc2626)"}},
		{"校验错误态 输入框", []string{".sky-node-test :user-invalid {", "box-shadow: 0 0 0 3px rgba(220, 38, 38, .12)"}},
		{"提交按钮", []string{".sky-node-test .sky-form-submit {", "padding: 11px 24px", "cursor: pointer"}},
		{"提交按钮 hover", []string{".sky-node-test .sky-form-submit:hover {", "var(--sky-btn-hover-bg"}},
	}
	for _, c := range cases {
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s：产物 CSS 里找不到 %q", c.desc, w)
			}
		}
	}

	// 占位指令必须被展开成真实声明 —— 原样漏进产物就是无效 CSS。
	if strings.Contains(out, "@focus-") {
		t.Errorf("占位指令没有展开，产物里残留: %s", out)
	}
	// 作用域必须生效：不能出现裸的顶层 & 或未替换的选择器。
	if strings.Contains(out, "&") {
		t.Errorf("产物里残留未替换的 & : %s", out)
	}
}

// TestFormCSSRejectsBadSource 解析器遇到不认识的写法必须报错，而不是静默跳过。
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
