package upload_test

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/upload"

	"github.com/spf13/viper"
)

func TestUploadRejectsFileOverMaxSize(t *testing.T) {
	cfg := viper.New()
	cfg.Set("upload.enabled", true)
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.max_size", "10B")
	cfg.Set("upload.allowed_extensions", []string{".txt"})
	cfg.Set("upload.allowed_mime_types", []string{"text/plain"})

	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() {
		if err := upload.Close(); err != nil {
			t.Fatalf("关闭上传组件失败: %v", err)
		}
	})

	_, err := upload.Upload(context.Background(), upload.File{
		Filename:    "demo.txt",
		Reader:      strings.NewReader("01234567890"),
		Size:        11,
		ContentType: "text/plain",
	}, upload.Request{})
	if err == nil {
		t.Fatalf("超出大小限制应返回错误")
	}
}

func TestUploadRejectsDisallowedExtension(t *testing.T) {
	cfg := viper.New()
	cfg.Set("upload.enabled", true)
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.max_size", "1MB")
	cfg.Set("upload.allowed_extensions", []string{".txt"})
	cfg.Set("upload.allowed_mime_types", []string{"text/plain"})

	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() {
		if err := upload.Close(); err != nil {
			t.Fatalf("关闭上传组件失败: %v", err)
		}
	})

	_, err := upload.Upload(context.Background(), upload.File{
		Filename:    "demo.exe",
		Reader:      strings.NewReader("abc"),
		Size:        3,
		ContentType: "text/plain",
	}, upload.Request{})
	if err == nil {
		t.Fatalf("非法扩展名应返回错误")
	}
}

func TestUploadRejectsDisallowedMimeType(t *testing.T) {
	cfg := viper.New()
	cfg.Set("upload.enabled", true)
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.max_size", "1MB")
	cfg.Set("upload.allowed_extensions", []string{".txt"})
	cfg.Set("upload.allowed_mime_types", []string{"text/plain"})

	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() {
		if err := upload.Close(); err != nil {
			t.Fatalf("关闭上传组件失败: %v", err)
		}
	})

	_, err := upload.Upload(context.Background(), upload.File{
		Filename:    "demo.txt",
		Reader:      strings.NewReader("abc"),
		Size:        3,
		ContentType: "application/octet-stream",
	}, upload.Request{})
	if err == nil {
		t.Fatalf("非法 MIME 类型应返回错误")
	}
}

func TestUploaderFacadeUsesFixedRequestAndProvider(t *testing.T) {
	cfg := viper.New()
	cfg.Set("upload.enabled", true)
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.max_size", "1MB")
	cfg.Set("upload.allowed_extensions", []string{".txt"})
	cfg.Set("upload.allowed_mime_types", []string{"text/plain"})

	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() {
		if err := upload.Close(); err != nil {
			t.Fatalf("关闭上传组件失败: %v", err)
		}
	})

	uploader := upload.NewUploader("local", upload.Request{
		Route:        "docs",
		Directory:    "reports",
		PreserveName: true,
	})

	result, err := uploader.Upload(context.Background(), upload.File{
		Filename:    "report.txt",
		Reader:      strings.NewReader("abc"),
		Size:        3,
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("通过 uploader facade 上传失败: %v", err)
	}

	if !strings.Contains(result.Key, "docs/reports/") {
		t.Fatalf("结果 key 不包含固定 request 路径: %s", result.Key)
	}
}
