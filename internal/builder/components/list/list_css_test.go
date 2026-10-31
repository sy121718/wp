package list

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestListCSSBase(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {\n  list-style: none;",
		".sky-c-t .sky-list-item {",
		"padding: calc(10px / 2) 0",
		".sky-c-t .sky-list-marker {",
		"width: 1.3em",
		".sky-c-t .sky-list-marker svg {",
		".sky-c-t .sky-list-text {",
		"min-width: 0",
		".sky-c-t a.sky-list-text {",
		".sky-c-t a.sky-list-text:hover {\n  text-decoration: underline;",
		".sky-c-t .sky-list-dot {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	for _, nw := range []string{
		"width: 1.8em",
		"font-size:",
		".sky-c-t .sky-list-marker {\n  color:",
		".sky-c-t .sky-list-text {\n  color:",
		".sky-c-t a.sky-list-text {\n  color:",
	} {
		if strings.Contains(out, nw) {
			t.Errorf("默认配置不该产出 %q:\n%s", nw, out)
		}
	}
}

func TestListCSSOptionals(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{
		IconColor:        "#f00",
		IconBgColor:      "#eee",
		IconSize:         "18px",
		IconColorHover:   "#0ff",
		IconBgColorHover: "#333",
		TextColor:        "#333333",
		TextSize:         "15px",
		LinkColor:        "#0000ff",
		LinkColorHover:   "#00ff00",
		Align:            "right",
		Spacing:          "20px",
	}, &b)
	out := b.String()
	for _, want := range []string{
		"padding: calc(20px / 2) 0",
		".sky-c-t .sky-list-marker {\n  color: #f00;",
		"background: #eee",
		"border-radius: var(--sky-radius-pill, 999px)",
		"width: 1.8em",
		".sky-c-t .sky-list-item:hover .sky-list-marker {",
		"color: #0ff",
		"background: #333",
		".sky-c-t .sky-list-text {\n  color: #333333;",
		"font-size: 15px",
		".sky-c-t a.sky-list-text {",
		"color: #0000ff",
		"color: #00ff00",
		"font-size: 18px",
		"align-items: flex-end",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}
