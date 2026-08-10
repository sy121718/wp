package auth_test

import (
	"testing"

	"go_wp/pkg/auth"

	"github.com/spf13/viper"
)

func TestAuthInitAndClose(t *testing.T) {
	t.Cleanup(func() {
		if err := auth.Close(); err != nil {
			t.Fatalf("关闭 JWT 失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("jwt.secret", "custom-secret")
	cfg.Set("jwt.expire_time", 24)
	cfg.Set("jwt.issuer", "go_wp")

	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化 JWT 失败: %v", err)
	}

	if err := auth.MustBeReady(); err != nil {
		t.Fatalf("JWT 初始化后应可用: %v", err)
	}

	if err := auth.Close(); err != nil {
		t.Fatalf("关闭 JWT 失败: %v", err)
	}

	if err := auth.MustBeReady(); err == nil {
		t.Fatalf("关闭后 JWT 不应继续可用")
	}
}
