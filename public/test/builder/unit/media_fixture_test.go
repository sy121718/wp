package unit

import (
	"fmt"
	"go_wp/internal/builder"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go_wp/pkg/upload"
)

const testMediaBaseURL = "https://media.example.test/storage"

// initFixtureMedia 显式注入生产媒体单源，历史 /storage 输入必须编译成完整地址。
func initFixtureMedia() error {
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.base_url", testMediaBaseURL)
	return upload.Init(cfg)
}

func TestMain(m *testing.M) {
	if err := initFixtureMedia(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if err := upload.Close(); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// TestMediaFixtureTrustedBoundary 覆盖完整产物、无基址拒绝和未知相对路径拒绝。
func TestMediaFixtureTrustedBoundary(t *testing.T) {
	local := mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"trusted-image","type":"core.image","props":{"src":"/storage/legacy.jpg"}}]}`)
	published, err := compile(t, local, builder.WithCompileMode(builder.CompileModePublish))
	if err != nil || !strings.Contains(published.HTML, `src="`+testMediaBaseURL+`/legacy.jpg"`) {
		t.Fatalf("历史存储地址必须经可信基址发布完整 URL: err=%v result=%v", err, published)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := initFixtureMedia(); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := compile(t, local, builder.WithCompileMode(builder.CompileModePublish)); err == nil || !strings.Contains(err.Error(), "完整 HTTP(S) 地址") {
		t.Fatalf("没有媒体基址时不能发布相对地址: %v", err)
	}
	if err := initFixtureMedia(); err != nil {
		t.Fatal(err)
	}
	unknown := mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"unknown-image","type":"core.image","props":{"src":"/images/not-owned.jpg"}}]}`)
	if _, err := compile(t, unknown, builder.WithCompileMode(builder.CompileModePublish)); err == nil || !strings.Contains(err.Error(), "完整 HTTP(S) 地址") {
		t.Fatalf("可信媒体基址不能替未知相对路径猜域名: %v", err)
	}
}
