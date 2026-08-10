package lock

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestLocalLockerAcquireAndRelease(t *testing.T) {
	locker := NewLocal()
	ctx := context.Background()

	lease, err := locker.Acquire(ctx, "task", time.Second)
	if err != nil {
		t.Fatalf("获取本地锁失败: %v", err)
	}
	if _, err := locker.Acquire(ctx, "task", time.Second); err == nil {
		t.Fatalf("同 key 重复获取本地锁应失败")
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatalf("释放本地锁失败: %v", err)
	}
	if _, err := locker.Acquire(ctx, "task", time.Second); err != nil {
		t.Fatalf("释放后应可重新获取本地锁: %v", err)
	}
}

func TestRedisLockerAcquireAndRelease(t *testing.T) {
	miniRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer miniRedis.Close()

	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{miniRedis.Addr()},
	})
	defer client.Close()

	locker := NewRedis(client, "lock:")
	ctx := context.Background()

	lease, err := locker.Acquire(ctx, "task", time.Second)
	if err != nil {
		t.Fatalf("获取 redis 锁失败: %v", err)
	}
	if _, err := locker.Acquire(ctx, "task", time.Second); err == nil {
		t.Fatalf("同 key 重复获取 redis 锁应失败")
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatalf("释放 redis 锁失败: %v", err)
	}
	if _, err := locker.Acquire(ctx, "task", time.Second); err != nil {
		t.Fatalf("释放后应可重新获取 redis 锁: %v", err)
	}
}
