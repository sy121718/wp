package templates

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func renderIconTemplate(t *testing.T, src string, vars jet.VarMap) string {
	t.Helper()
	loader := jet.NewInMemLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	injectGlobals(set)
	loader.Set("t.jet", src)
	tpl, err := set.GetTemplate("t.jet")
	if err != nil {
		t.Fatalf("取模板失败：%v", err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, vars, nil); err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	return sb.String()
}

func TestIconEmitsCatalogSVG(t *testing.T) {
	got := renderIconTemplate(t, `<span>{{ icon("mail") }}</span>`, jet.VarMap{})
	if !strings.Contains(got, "<svg") || !strings.Contains(got, "</svg>") {
		t.Fatalf("已知图标应输出内联 SVG，实际：%s", got)
	}
	if strings.Contains(got, "&lt;svg") {
		t.Fatalf("SVG 被转义了，说明 icon 没有走 SafeWriter：%s", got)
	}
}

func TestIconRejectsUnknownAndMaliciousNames(t *testing.T) {
	for _, name := range []string{
		"", "not-a-real-icon", "../../etc/passwd", "icons/mail",
		`"><script>alert(1)</script>`, "<svg onload=alert(1)>",
	} {
		got := renderIconTemplate(t, `<span>{{ icon(name) }}</span>`, jet.VarMap{}.Set("name", name))
		if strings.Contains(got, "<script") || strings.Contains(got, "onload") || strings.Contains(got, "passwd") {
			t.Errorf("恶意/未知图标名 %q 产生了活动内容：%s", name, got)
		}
		if got != "<span></span>" {
			t.Errorf("未知图标名 %q 应输出空，实际：%q", name, got)
		}
	}
}
