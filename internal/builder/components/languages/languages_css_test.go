package languages

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestLanguagesCSSCurrentMirrorsLink(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Color: "#333", FontSize: "14px", FontWeight: "600", ItemPadding: "4px 8px"}, &b)
	out := b.String()
	link := blockOf(out, ".sky-c-t .sky-lang-link {")
	current := blockOf(out, ".sky-c-t .sky-lang-current {")
	if link == "" || current == "" {
		t.Fatalf("找不到两条规则:\n%s", out)
	}
	for _, decl := range []string{"display: inline-flex", "align-items: center", "text-decoration: none", "transition: color .15s ease", "color: #333", "font-size: 14px", "font-weight: 600", "padding: 4px 8px"} {
		if !strings.Contains(link, decl) || !strings.Contains(current, decl) {
			t.Errorf("排版声明 %q 未同时出现在两条规则里\nlink:\n%s\ncurrent:\n%s", decl, link, current)
		}
	}
	if !strings.Contains(current, "cursor: default") {
		t.Errorf("当前语言缺少 cursor: default:\n%s", current)
	}
	if strings.Contains(link, "cursor: default") {
		t.Errorf("链接不该带 cursor: default:\n%s", link)
	}
}

func TestLanguagesCSSOptionals(t *testing.T) {
	var plain core.CSSBuckets
	compileCSS("t", &Props{}, &plain)
	out := plain.String()
	for _, nw := range []string{"flex-direction: column", "gap:", ":hover", "cursor: default\n}"} {
		if strings.Contains(out, nw) {
			t.Errorf("默认配置不该产出 %q:\n%s", nw, out)
		}
	}

	var b core.CSSBuckets
	compileCSS("t", &Props{Orientation: "vertical", Gap: "8px", HoverColor: "#00f", CurrentColor: "#999"}, &b)
	out2 := b.String()
	for _, want := range []string{"flex-direction: column", "align-items: flex-start", "gap: 8px", ".sky-c-t .sky-lang-link:hover", "color: #00f", "color: #999"} {
		if !strings.Contains(out2, want) {
			t.Errorf("产物缺少 %q\n%s", want, out2)
		}
	}
}

func blockOf(out, header string) string {
	i := strings.Index(out, header)
	if i < 0 {
		return ""
	}
	rest := out[i:]
	if j := strings.Index(rest[1:], "\n\n"); j >= 0 {
		return rest[:j+1]
	}
	return rest
}
