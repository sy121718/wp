package uploadprovider

import (
	"github.com/spf13/viper"
	"strings"
	"testing"
)

func TestMediaBaseURL(t *testing.T) {
	for _, tc := range []struct {
		raw, mode string
		port      int
		want      string
	}{
		{"https://example.com", "release", 0, "https://example.com/storage"},
		{"https://cdn.example.com/storage/", "release", 0, "https://cdn.example.com/storage"},
		{"https://cdn.example.com/shop", "release", 0, "https://cdn.example.com/shop/storage"},
		{"", "debug", 19128, "http://127.0.0.1:19128/storage"},
		{"", "test", 8080, "http://127.0.0.1:8080/storage"},
	} {
		v := viper.New()
		v.Set("upload.base_url", tc.raw)
		v.Set("server.mode", tc.mode)
		v.Set("server.port", tc.port)
		got, err := MediaBaseURL(v)
		if err != nil || got != tc.want {
			t.Errorf("%+v: got=%q err=%v", tc, got, err)
		}
	}
	for _, raw := range []string{"", "/storage", "//cdn.example.com", "javascript:alert(1)", "https://user:pass@cdn.example.com", "https://cdn.example.com?key=value", "https://cdn.example.com/#frag", "https://cdn.example.com\\evil"} {
		v := viper.New()
		v.Set("upload.base_url", raw)
		v.Set("server.mode", "release")
		if got, err := MediaBaseURL(v); err == nil {
			t.Errorf("无效生产配置 %q 未拒绝: %q", raw, got)
		}
	}
}

func TestLocalUploadUsesSameBaseURL(t *testing.T) {
	p, _ := newTempLocalProvider(t)
	got, err := uploadBody(p, "same-base.txt", "内容")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.URL, "https://media.example.test/storage/") {
		t.Fatalf("上传返回了不同媒体前缀: %q", got.URL)
	}
}
