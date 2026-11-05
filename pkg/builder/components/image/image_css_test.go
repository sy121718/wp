package image

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestImageCSSBase(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{
		AspectRatio:    "16:9",
		ObjectFit:      "contain",
		ObjectPosition: "top center",
		Width:          "80%",
		MaxWidth:       "600px",
		BorderRadius:   "12px",
		Height:         Responsive{Desktop: "300px", Mobile: "200px"},
		Align:          Align{Desktop: "center", Tablet: "right"},
		Filters:        Filters{Brightness: 120},
	}, &b)
	out := b.String()
	for _, want := range []string{
		"object-fit: contain",
		"object-position: top center",
		"aspect-ratio: 16 / 9",
		"width: 80%",
		"max-width: 600px",
		"height: 300px",
		"display: block",
		"margin-left: auto",
		"margin-right: auto",
		"border-radius: 12px",
		"filter: brightness(120%)",
		"height: 200px",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}

func TestImageCSSObjectFitCoverOmitted(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{ObjectFit: "cover"}, &b)
	if strings.Contains(b.String(), "object-fit") {
		t.Errorf("cover 不该产出 object-fit:\n%s", b.String())
	}
}

func TestImageCSSHoverPair(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Hover: Hover{Scale: "1.05", RestoreColor: true, Duration: "200ms"}}, &b)
	out := b.String()
	for _, want := range []string{
		"transition: transform 200ms ease, filter 200ms ease",
		"@media (hover: hover)",
		"transform: scale(1.05)",
		"filter: none",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	var none core.CSSBuckets
	compileCSS("t", &Props{}, &none)
	if strings.Contains(none.String(), "transition") || strings.Contains(none.String(), "hover: hover") {
		t.Errorf("未配置悬浮却产出了过渡或 hover 规则:\n%s", none.String())
	}
}

func TestImageCSSLightboxIsGlobal(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-lightbox {\n  display: none;",
		".sky-lightbox:target {\n  display: flex;",
		".sky-lightbox img {",
		".sky-lightbox-close {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if strings.Contains(out, ".sky-c-t .sky-lightbox") {
		t.Errorf("灯箱规则被错误地作用域化了:\n%s", out)
	}

	compileCSS("t2", &Props{}, &b)
	if n := strings.Count(b.String(), ".sky-lightbox {\n  display: none;"); n != 1 {
		t.Errorf("灯箱浮层应只产出一份，实际 %d 份", n)
	}
}
