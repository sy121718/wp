package builder

import (
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

var loaderVariants = []struct {
	variant string
	html    string
	self    string
}{
	{"spinner", `<span class="sky-loader-ring" aria-hidden="true"></span>`, "ring"},
	{"dots", strings.Repeat(`<i class="sky-loader-dot" aria-hidden="true"></i>`, 3), "dot"},
	{"bars", strings.Repeat(`<i class="sky-loader-bar" aria-hidden="true"></i>`, 4), "bar"},
	{"pulse", `<span class="sky-loader-pulse" aria-hidden="true"></span>`, "pulse"},
	{"plane", `<span class="sky-loader-plane" aria-hidden="true"></span>`, "plane"},
	{"grid", `<span class="sky-loader-grid" aria-hidden="true">` + strings.Repeat("<i></i>", 9) + `</span>`, "grid"},
	{"orbit", `<span class="sky-loader-orbit" aria-hidden="true"></span>`, "orbit"},
	{"wave", `<span class="sky-loader-wave" aria-hidden="true">` + strings.Repeat("<i></i>", 5) + `</span>`, "wave"},
	{"bounce", strings.Repeat(`<i class="sky-loader-bounce" aria-hidden="true"></i>`, 3), "bounce"},
}

func loaderDocJSON() string {
	parts := make([]string, 0, len(loaderVariants))
	for i, v := range loaderVariants {
		parts = append(parts, fmt.Sprintf(`{"id": "ld%d", "type": "core.loader", "props": {"variant": %q}}`, i+1, v.variant))
	}
	return `{"settings": {"layout": {"mode": "full"}, "seo": {"title": "loader", "description": "loader"}}, "root": [` +
		strings.Join(parts, ",") + `]}`
}

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

func TestCompileLoaderVariantsHTML(t *testing.T) {
	res := compileLoaderDoc(t)
	for _, v := range loaderVariants {
		if !strings.Contains(res.HTML, v.html) {
			t.Errorf("形态 %s：产物 HTML 缺少期望结构 %s", v.variant, v.html)
		}
	}
}

func TestCompileLoaderVariantsCSS(t *testing.T) {
	res := compileLoaderDoc(t)
	for i, v := range loaderVariants {
		sel := fmt.Sprintf(".sky-c-ld%d .sky-loader-%s", i+1, v.self)
		if !strings.Contains(res.CSS, sel) {
			t.Errorf("形态 %s：产物 CSS 缺少选择器 %s", v.variant, sel)
		}
	}
	for _, want := range []string{"@keyframes sky-loader-wave", "@keyframes sky-loader-bounce"} {
		if !strings.Contains(res.CSS, want) {
			t.Errorf("产物 CSS 缺少 %s", want)
		}
	}
	if !strings.Contains(res.CSS, "animation-delay: -1.2s") {
		t.Error("波浪条相位错开的负延迟未进产物")
	}
}
