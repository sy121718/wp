package upload

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/viper"
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
}

// webpHead RIFF....WEBPVP：Go 的 http.DetectContentType 认得它。
var webpHead = []byte{'R', 'I', 'F', 'F', 0x1a, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}

func TestEffectiveMIMEPrefersSniffedContent(t *testing.T) {
	cases := []struct {
		name     string
		declared string
		head     []byte
		want     string
	}{
		// 回归：curl 上传 .webp 时声明的是 application/octet-stream，只信声明值会让
		// 合法图片整批被拒（实测 449 张图里 178 张 webp 全部失败）。
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

// TestMediaBaseURLFromConfig 钉住 upload.base_url → 对外前缀的拼接口径。
//
// 与 provider 侧 local.go 的 "siteURL + /storage" 必须逐字一致：两处拼法分叉时，
// 新上传拿到一种前缀、历史附件派生另一种，表现是「同一张图两个地址」。
func TestMediaBaseURLFromConfig(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"未配置", "", "/storage"},
		{"站点根", "http://host:8080", "http://host:8080/storage"},
		{"站点根带尾斜杠", "https://www.example.com/", "https://www.example.com/storage"},
		{"已含 storage 段不重复追加", "https://cdn.example.com/storage", "https://cdn.example.com/storage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := viper.New()
			if tc.raw != "" {
				v.Set("upload.base_url", tc.raw)
			}
			if got := mediaBaseURLFromConfig(v); got != tc.want {
				t.Fatalf("mediaBaseURLFromConfig(%q) = %q，期望 %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestStorageURL 钉住「存储标识 → 对外 URL」的归一（未配 base_url 的缺省形态）。
//
// 三种历史写法都要认，且**换了主机名要跟着配置走**：库里与 Page Document 里
// 存在配过旧 base_url 的绝对地址，若原样返回，换域名后它们会永远指着旧主机。
func TestStorageURL(t *testing.T) {
	old := mediaBaseURL
	mediaBaseURL = defaultMediaBaseURL
	t.Cleanup(func() { mediaBaseURL = old })

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空串给空串", "", ""},
		{"纯存储键", "482.jpg", "/storage/482.jpg"},
		{"相对 URL", "/storage/482.jpg", "/storage/482.jpg"},
		{"无前导斜杠的 storage 段", "storage/482.jpg", "/storage/482.jpg"},
		{"历史绝对地址按配置重拼", "http://old-host/storage/482.jpg", "/storage/482.jpg"},
		{"变体文件同样归一", "http://old-host/storage/482_thumb.jpg", "/storage/482_thumb.jpg"},
		{"外链原样返回", "https://cdn.example.com/x.jpg", "https://cdn.example.com/x.jpg"},
		{"协议相对原样返回", "//cdn.example.com/x.jpg", "//cdn.example.com/x.jpg"},
		{"data URI 原样返回", "data:image/png;base64,AAAA", "data:image/png;base64,AAAA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StorageURL(tc.in); got != tc.want {
				t.Fatalf("StorageURL(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStorageURLWithBaseURL 配了站点根时，所有形态都要带上域名。
func TestStorageURLWithBaseURL(t *testing.T) {
	old := mediaBaseURL
	mediaBaseURL = "https://www.example.com/storage"
	t.Cleanup(func() { mediaBaseURL = old })

	for _, in := range []string{"482.jpg", "/storage/482.jpg", "http://old-host/storage/482.jpg"} {
		if got, want := StorageURL(in), "https://www.example.com/storage/482.jpg"; got != want {
			t.Fatalf("StorageURL(%q) = %q，期望 %q", in, got, want)
		}
	}
	if got := BaseURL(); got != "https://www.example.com/storage" {
		t.Fatalf("BaseURL() = %q", got)
	}
	// 外链不受 base_url 影响。
	if got := StorageURL("https://cdn.example.com/x.jpg"); got != "https://cdn.example.com/x.jpg" {
		t.Fatalf("外链被改写: %q", got)
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

	// 声明成 webp、内容却是 PNG —— 判据是内容，应被拒。
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	err := validateFile(File{Filename: "a.webp", ContentType: "image/webp"}, png)
	if err == nil {
		t.Fatal("内容与白名单不符时应拒绝")
	}
	if !strings.Contains(err.Error(), "image/png") {
		t.Fatalf("错误信息应带上实际判定出的类型，实际: %v", err)
	}
}
