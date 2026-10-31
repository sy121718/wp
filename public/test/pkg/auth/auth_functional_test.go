package auth_test

import (
	"context"
	"testing"
	"time"

	"go_wp/pkg/auth"
	"go_wp/pkg/cache"

	"github.com/alicebob/miniredis/v2"
	"github.com/spf13/viper"
)

func setupAuthWithMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	miniRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	t.Cleanup(miniRedis.Close)

	cfg := viper.New()
	cfg.Set("redis.enabled", true)
	cfg.Set("redis.addrs", []string{miniRedis.Addr()})
	cfg.Set("auth.session_secret", "custom-secret")

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化缓存失败: %v", err)
	}
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化会话存储失败: %v", err)
	}
	t.Cleanup(func() {
		_ = auth.Close()
		_ = cache.Close()
	})
	return miniRedis
}

func TestAuthBlockUserUsesFixedTTL(t *testing.T) {
	miniRedis := setupAuthWithMiniRedis(t)
	ctx := context.Background()

	if err := auth.BlockUser(ctx, 42, time.Now()); err != nil {
		t.Fatalf("BlockUser(now) 不应因负 TTL 报错: %v", err)
	}

	ttl := miniRedis.TTL("user:blocked:42")
	if ttl <= 0 || ttl < 6*24*time.Hour || ttl > 7*24*time.Hour {
		t.Fatalf("封禁 key TTL 应为固定 7 天: got=%v", ttl)
	}
}

func TestAuthIsBlockedSemantics(t *testing.T) {
	_ = setupAuthWithMiniRedis(t)
	ctx := context.Background()

	now := time.Now()
	if err := auth.BlockUser(ctx, 7, now); err != nil {
		t.Fatalf("BlockUser 失败: %v", err)
	}

	blocked, err := auth.IsBlocked(ctx, 7, now.Add(-10*time.Second).Unix())
	if err != nil {
		t.Fatalf("IsBlocked 返回错误: %v", err)
	}
	if !blocked {
		t.Fatalf("封禁时间戳之前的会话应被拒绝")
	}

	blocked, err = auth.IsBlocked(ctx, 7, now.Add(10*time.Second).Unix())
	if err != nil {
		t.Fatalf("IsBlocked 返回错误: %v", err)
	}
	if blocked {
		t.Fatalf("封禁时间戳之后的会话不应被拒绝")
	}

	blocked, err = auth.IsBlocked(ctx, 99, now.Unix())
	if err != nil {
		t.Fatalf("未封禁用户不应返回错误: %v", err)
	}
	if blocked {
		t.Fatalf("未封禁用户不应被判为封禁")
	}
}

func TestAuthIsBlockedFailClosedOnRedisError(t *testing.T) {
	miniRedis := setupAuthWithMiniRedis(t)
	ctx := context.Background()

	miniRedis.Close()

	if _, err := auth.IsBlocked(ctx, 1, 1); err == nil {
		t.Fatalf("Redis 不可用时 IsBlocked 应返回错误（fail-closed），不能吞错放行")
	}
	if err := auth.BlockUser(ctx, 1, time.Now()); err == nil {
		t.Fatalf("Redis 不可用时 BlockUser 应返回错误")
	}
}

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
