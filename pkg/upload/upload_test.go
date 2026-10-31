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

	head := bytes.TrimSpace([]byte("\n\n<svg><rect/></svg>"))
	got := detectDangerousContent(head)
	if got == "" {
		t.Fatal("带前导空白的 <svg> 前缀应被拒绝")
	}
}

var webpHead = []byte{'R', 'I', 'F', 'F', 0x1a, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}

func TestEffectiveMIMEPrefersSniffedContent(t *testing.T) {
	cases := []struct {
		name     string
		declared string
		head     []byte
		want     string
	}{

		{"webp 被声明为 octet-stream", "application/octet-stream", webpHead, "image/webp"},
		{"webp 声明正确", "image/webp", webpHead, "image/webp"},
		{"png 头优先于错误声明", "text/plain", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"嗅探不出时回落声明值", "application/pdf", []byte{0x00, 0x00, 0x00, 0x00}, "application/pdf"},
		{"无文件头时回落声明值", "image/gif", nil, "image/gif"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveMIME(File{ContentType: tc.declared}, tc.head)
			if got != tc.want {
				t.Fatalf("effectiveMIME(声明=%q, head=%v) = %q，期望 %q", tc.declared, tc.head, got, tc.want)
			}
		})
	}
}

func TestValidateFileAcceptsWebpDeclaredAsOctetStream(t *testing.T) {
	old := uploadRules
	uploadRules = validationRules{
		maxSize:           10 * 1024 * 1024,
		allowedExtensions: map[string]struct{}{".webp": {}},
		allowedMIMETypes:  map[string]struct{}{"image/webp": {}},
	}
	t.Cleanup(func() { uploadRules = old })

	if err := validateFile(File{Filename: "a.webp", ContentType: "application/octet-stream"}, webpHead); err != nil {
		t.Fatalf("内容确为 webp 时应通过，实际被拒: %v", err)
	}
}

func TestStorageURL(t *testing.T) {
	old := mediaBaseURL
	mediaBaseURL = "https://www.example.com/storage"
	t.Cleanup(func() { mediaBaseURL = old })
	cases := []struct{ in, want string }{
		{"", ""},
		{"482.jpg", "https://www.example.com/storage/482.jpg"},
		{"/storage/482.jpg", "https://www.example.com/storage/482.jpg"},
		{"/storage/482.jpg?v=2#photo", "https://www.example.com/storage/482.jpg?v=2#photo"},
		{"/uploads/482.jpg", ""},
		{"storage/482.jpg", "https://www.example.com/storage/482.jpg"},
		{"folder/a b.jpg", "https://www.example.com/storage/folder/a%20b.jpg"},
		{"https://external.example/storage/482.jpg", "https://external.example/storage/482.jpg"},
		{"https://cdn.example.com/x.jpg", "https://cdn.example.com/x.jpg"},
		{"//cdn.example.com/x.jpg", "https://cdn.example.com/x.jpg"},
		{"data:image/png;base64,AAAA", ""},
		{"javascript:alert(1)", ""},
		{"blob:https://www.example.com/id", ""},
		{"../escape.jpg", ""},
		{"/storage/%2e%2e/escape.jpg", ""},
	}
	for _, tc := range cases {
		if got := StorageURL(tc.in); got != tc.want {
			t.Errorf("StorageURL(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
	for _, raw := range []string{"482.jpg", "/storage/482.jpg", "https://www.example.com/storage/482.jpg?v=1#image"} {
		if key := StorageKey(raw); key != "482.jpg" {
			t.Errorf("StorageKey(%q)=%q", raw, key)
		}
	}
	if key := StorageKey("https://external.example/storage/482.jpg"); key != "" {
		t.Fatalf("外链不应被认作本地附件: %q", key)
	}
	mediaBaseURL = ""
	if got := StorageURL("482.jpg"); got != "" {
		t.Fatalf("未初始化不能退回相对路径: %q", got)
	}
}

func TestValidateFileRejectsMismatchedContent(t *testing.T) {
	old := uploadRules
	uploadRules = validationRules{
		maxSize:           10 * 1024 * 1024,
		allowedExtensions: map[string]struct{}{".webp": {}},
		allowedMIMETypes:  map[string]struct{}{"image/webp": {}},
	}
	t.Cleanup(func() { uploadRules = old })

	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	err := validateFile(File{Filename: "a.webp", ContentType: "image/webp"}, png)
	if err == nil {
		t.Fatal("内容与白名单不符时应拒绝")
	}
	if !strings.Contains(err.Error(), "image/png") {
		t.Fatalf("错误信息应带上实际判定出的类型，实际: %v", err)
	}
}
