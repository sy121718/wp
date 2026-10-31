package text

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestTextCSSLinkColorIsSeparateRule(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Color: "#123456", LinkColor: "#ff0000"}, &b)
	out := b.String()
	if !strings.Contains(out, ".sky-c-t {\n  color: #123456;") {
		t.Errorf("正文色应落在容器上:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t a {\n  color: #ff0000;") {
		t.Errorf("链接色应落在后代 a 上:\n%s", out)
	}
}

func TestTextCSSOptionalDecls(t *testing.T) {
	var plain core.CSSBuckets
	compileCSS("t", &Props{}, &plain)
	if out := strings.TrimSpace(plain.String()); out != "" {
		t.Errorf("全空的正文组件不应产出任何规则，实际:\n%s", out)
	}

	var b core.CSSBuckets
	compileCSS("t", &Props{ParagraphSpacing: "12px", LineClamp: 3}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t p, .sky-c-t ul, .sky-c-t ol, .sky-c-t blockquote {",
		"margin-top: 12px",
		"margin-bottom: 12px",
		"-webkit-line-clamp: 3",
		"display: -webkit-box",
		"overflow: hidden",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}
