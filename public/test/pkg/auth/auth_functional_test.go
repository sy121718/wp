package auth_test

import (
	"testing"

	"go_wp/pkg/auth"

	"github.com/spf13/viper"
)

func TestAuthInitAndClose(t *testing.T) {
	t.Cleanup(func() {
		if err := auth.Close(); err != nil {
			t.Fatalf("关闭会话存储失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("auth.session_secret", "custom-secret")

	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化会话存储失败: %v", err)
	}

	if err := auth.Ready(); err != nil {
		t.Fatalf("会话存储初始化后应可用: %v", err)
	}

	if err := auth.Close(); err != nil {
		t.Fatalf("关闭会话存储失败: %v", err)
	}

	if err := auth.Ready(); err == nil {
		t.Fatalf("关闭后会话存储不应继续可用")
	}
}
