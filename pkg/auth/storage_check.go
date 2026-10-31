package auth

import (
	"errors"

	"go_wp/pkg/cache"
)

func RequireSessionStorage() error {
	if !cache.IsInited() {
		return errors.New("认证会话存储不可用：请在配置中启用 redis（redis.enabled=true）")
	}
	return nil
}
