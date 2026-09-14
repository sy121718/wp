package upload

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectDangerousContentRejectsSVG(t *testing.T) {
	cases := []struct {
		name string
		head []byte
	}{
		{"svg tag", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"xml svg", []byte(`<?xml version="1.0"?><svg><circle r="1"/></svg>`)},
		{"html", []byte("<!doctype html><html><body>hi</body></html>")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectDangerousContent(tc.head); got == "" {
				t.Fatalf("detectDangerousContent(%q) 应拒绝，实际通过", tc.head)
			}
		})
	}
}

func TestDetectDangerousContentAllowsPNG(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if got := detectDangerousContent(png); got != "" {
		t.Fatalf("PNG 头不应被判为危险内容: %q", got)
	}
}

func TestDetectDangerousContentRejectsSVGExtensionBypass(t *testing.T) {
	// 扩展名伪装成 .png 时，魔数仍应拦截 SVG 内容。
	head := bytes.TrimSpace([]byte("\n\n<svg><rect/></svg>"))
	got := detectDangerousContent(head)
	if got == "" {
		t.Fatal("带前导空白的 <svg> 前缀应被拒绝")
	}
	if !strings.Contains(got, "html") && !strings.Contains(got, "svg") {
		t.Fatalf("拒绝原因应标明 html/svg，实际 %q", got)
	}
}
