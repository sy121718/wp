package config

import (
	"testing"

	"github.com/spf13/viper"
	"go_wp/pkg/upload"
)

func TestUploadUsesTrustedSiteBaseURL(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://site.example.test/shop")
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", t.TempDir())
	cfg.Set("server.mode", "release")
	if err := initUpload(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upload.Close() })
	if got := upload.StorageURL("photo.jpg"); got != "https://site.example.test/shop/storage/photo.jpg" {
		t.Fatal(got)
	}
	_ = upload.Close()
	cfg.Set("upload.base_url", "https://cdn.example.test/storage")
	if err := initUpload(cfg); err != nil {
		t.Fatal(err)
	}
	if got := upload.StorageURL("photo.jpg"); got != "https://cdn.example.test/storage/photo.jpg" {
		t.Fatal(got)
	}
}
