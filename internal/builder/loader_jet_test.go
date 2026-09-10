package builder

// loader_jet_test.go — 加载器九形态的链路级验证（ParsePage → Compile → 产物 HTML/CSS）。
// 单元级见 internal/builder/components/loader/loader_test.go；
// 本文件的期望值独立于实现里的 shapes 形态表（硬编码产物结构），
// 用来兜底「形状表自身写错」这种情况——模板分支与 CSS 却彼此自洽。

import (
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// loaderVariants 九形态 → 期望的产物 DOM 片段与 CSS 选择器后缀。
var loaderVariants = []struct {
	variant string
	html    string
	self    string
}{
	{"spinner", `<span class="wp-loader-ring" aria-hidden="true"></span>`, "ring"},
	{"dots", strings.Repeat(`<i class="wp-loader-dot" aria-hidden="true"></i>`, 3), "dot"},
	{"bars", strings.Repeat(`<i class="wp-loader-bar" aria-hidden="true"></i>`, 4), "bar"},
	{"pulse", `<span class="wp-loader-pulse" aria-hidden="true"></span>`, "pulse"},
	{"plane", `<span class="wp-loader-plane" aria-hidden="true"></span>`, "plane"},
	{"grid", `<span class="wp-loader-grid" aria-hidden="true">` + strings.Repeat("<i></i>", 9) + `</span>`, "grid"},
	{"orbit", `<span class="wp-loader-orbit" aria-hidden="true"></span>`, "orbit"},
	{"wave", `<span class="wp-loader-wave" aria-hidden="true">` + strings.Repeat("<i></i>", 5) + `</span>`, "wave"},
	{"bounce", strings.Repeat(`<i class="wp-loader-bounce" aria-hidden="true"></i>`, 3), "bounce"},
}

// loaderDocJSON 拼装九形态文档（节点 id 依次 ld1..ld9）。
func loaderDocJSON() string {
	parts := make([]string, 0, len(loaderVariants))
	for i, v := range loaderVariants {
		parts = append(parts, fmt.Sprintf(`{"id": "ld%d", "type": "core.loader", "props": {"variant": %q}}`, i+1, v.variant))
	}
	return `{"settings": {"layout": {"mode": "full"}, "seo": {"title": "loader", "description": "loader"}}, "root": [` +
		strings.Join(parts, ",") + `]}`
}

// compileLoaderDoc 编译九形态文档。
func compileLoaderDoc(t *testing.T) *CompiledPage {
	t.Helper()
	p, err := ParsePage([]byte(loaderDocJSON()))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	res, err := Compile(p, WithComponentSet(set))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res
}

// TestCompileLoaderVariantsHTML 九形态都产出各自的结构（模板漏写形态 = 这里红）。
func TestCompileLoaderVariantsHTML(t *testing.T) {
	res := compileLoaderDoc(t)
	for _, v := range loaderVariants {
		if !strings.Contains(res.HTML, v.html) {
			t.Errorf("形态 %s：产物 HTML 缺少期望结构 %s", v.variant, v.html)
		}
	}
}

// TestCompileLoaderVariantsCSS 九形态的样式与关键帧都进产物，且选择器对得上模板输出的类名。
func TestCompileLoaderVariantsCSS(t *testing.T) {
	res := compileLoaderDoc(t)
	for i, v := range loaderVariants {
		sel := fmt.Sprintf(".wp-c-ld%d .wp-loader-%s", i+1, v.self)
		if !strings.Contains(res.CSS, sel) {
			t.Errorf("形态 %s：产物 CSS 缺少选择器 %s", v.variant, sel)
		}
	}
	for _, want := range []string{"@keyframes wp-loader-wave", "@keyframes wp-loader-bounce"} {
		if !strings.Contains(res.CSS, want) {
			t.Errorf("产物 CSS 缺少 %s", want)
		}
	}
	if !strings.Contains(res.CSS, "animation-delay: -1.2s") {
		t.Error("波浪条相位错开的负延迟未进产物")
	}
}
