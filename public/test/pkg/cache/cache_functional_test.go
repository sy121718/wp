package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"go_wp/pkg/cache"

	"github.com/alicebob/miniredis/v2"
	"github.com/spf13/viper"
)

func TestCacheInitAndReadWriteWithMiniRedis(t *testing.T) {
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("关闭缓存失败: %v", err)
		}
	})

	miniRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer miniRedis.Close()

	cfg := viper.New()
	cfg.Set("redis.provider", "redis")
	cfg.Set("redis.addrs", []string{miniRedis.Addr()})
	cfg.Set("redis.password", "")
	cfg.Set("redis.db", 0)

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化缓存失败: %v", err)
	}

	if !cache.IsInited() {
		t.Fatalf("缓存初始化状态错误: 期望=true 实际=false")
	}

	client, err := cache.GetRedis()
	if err != nil {
		t.Fatalf("获取缓存客户端失败: %v", err)
	}

	ctx := context.Background()
	if err := client.Set(ctx, "k:feature", "v:ok", time.Minute).Err(); err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}

	value, err := client.Get(ctx, "k:feature").Result()
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if value != "v:ok" {
		t.Fatalf("缓存值不正确: 期望=%s 实际=%s", "v:ok", value)
	}
}

func TestCacheSetAndGetJSON(t *testing.T) {
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("关闭缓存失败: %v", err)
		}
	})

	miniRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer miniRedis.Close()

	cfg := viper.New()
	cfg.Set("redis.provider", "redis")
	cfg.Set("redis.addrs", []string{miniRedis.Addr()})

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化缓存失败: %v", err)
	}

	ctx := context.Background()
	payload := struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}{
		Name: "alice",
		Age:  18,
	}

	if err := cache.SetJSON(ctx, "user:1", payload, time.Minute); err != nil {
		t.Fatalf("写入 JSON 缓存失败: %v", err)
	}

	got, err := cache.GetJSON[struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}](ctx, "user:1")
	if err != nil {
		t.Fatalf("读取 JSON 缓存失败: %v", err)
	}

	if got.Name != "alice" || got.Age != 18 {
		t.Fatalf("JSON 缓存内容不正确: %+v", got)
	}
}

func TestCacheRememberJSONUsesSingleflight(t *testing.T) {
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("关闭缓存失败: %v", err)
		}
	})

	miniRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer miniRedis.Close()

	cfg := viper.New()
	cfg.Set("redis.provider", "redis")
	cfg.Set("redis.addrs", []string{miniRedis.Addr()})

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化缓存失败: %v", err)
	}

	ctx := context.Background()
	var mu sync.Mutex
	loadCount := 0
	loader := func(context.Context) (map[string]string, error) {
		mu.Lock()
		loadCount++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		return map[string]string{"value": "ok"}, nil
	}

	var wg sync.WaitGroup
	results := make([]map[string]string, 5)
	errors := make([]error, 5)
	for i := 0; i < 5; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[idx], errors[idx] = cache.RememberJSON(ctx, "sf:key", time.Minute, 0, loader)
		}()
	}
	wg.Wait()

	for _, err := range errors {
		if err != nil {
			t.Fatalf("RememberJSON 返回错误: %v", err)
		}
	}
	for _, result := range results {
		if result["value"] != "ok" {
			t.Fatalf("RememberJSON 结果不正确: %+v", result)
		}
	}
	if loadCount != 1 {
		t.Fatalf("singleflight 未生效: got=%d want=%d", loadCount, 1)
	}
}

func TestCacheRememberJSONCachesZeroValueResult(t *testing.T) {
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("关闭缓存失败: %v", err)
		}
	})

	miniRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer miniRedis.Close()

	cfg := viper.New()
	cfg.Set("redis.provider", "redis")
	cfg.Set("redis.addrs", []string{miniRedis.Addr()})

	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化缓存失败: %v", err)
	}

	ctx := context.Background()
	loadCount := 0
	loader := func(context.Context) (struct {
		Value string `json:"value"`
	}, error) {
		loadCount++
		return struct {
			Value string `json:"value"`
		}{}, nil
	}

	first, err := cache.RememberJSON(ctx, "zero:key", time.Minute, 0, loader)
	if err != nil {
		t.Fatalf("第一次 RememberJSON 失败: %v", err)
	}
	second, err := cache.RememberJSON(ctx, "zero:key", time.Minute, 0, loader)
	if err != nil {
		t.Fatalf("第二次 RememberJSON 失败: %v", err)
	}

	if first.Value != "" || second.Value != "" {
		t.Fatalf("零值缓存结果不正确: first=%+v second=%+v", first, second)
	}
	if loadCount != 1 {
		t.Fatalf("零值结果应被缓存: got=%d want=%d", loadCount, 1)
	}
}
