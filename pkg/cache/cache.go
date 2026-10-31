package cache

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"math/rand"
	"strings"
	"time"

	cacheprovider "go_wp/pkg/cache/provider"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"golang.org/x/sync/singleflight"
)

const defaultCacheProvider = "redis"

func buildProvider(v *viper.Viper) (cacheprovider.Provider, error) {
	providerName := defaultCacheProvider
	if v != nil {
		providerName = strings.TrimSpace(strings.ToLower(v.GetString("redis.provider")))
		if providerName == "" {
			providerName = defaultCacheProvider
		}
	}

	switch providerName {
	case defaultCacheProvider:
		return cacheprovider.NewRedisProvider(), nil
	default:
		return nil, fmt.Errorf("不支持的缓存 provider: %s", providerName)
	}
}

var defaultProvider cacheprovider.Provider = cacheprovider.NewRedisProvider()
var loadGroup singleflight.Group

func Init(v *viper.Viper) error {
	provider, err := buildProvider(v)
	if err != nil {
		return err
	}
	defaultProvider = provider
	return defaultProvider.Init(v)
}

func GetRedis() (redis.UniversalClient, error) {
	return defaultProvider.Client()
}

func SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	client, err := GetRedis()
	if err != nil {
		return err
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化缓存 JSON 失败: %w", err)
	}

	if err := client.Set(ctx, key, payload, ttl).Err(); err != nil {
		return fmt.Errorf("写入缓存失败: %w", err)
	}
	return nil
}

func SetJSONKeepTTL(ctx context.Context, key string, value any) error {
	_, err := setJSONKeepTTL(ctx, key, value, false)
	return err
}

func SetJSONKeepTTLIfExists(ctx context.Context, key string, value any) (bool, error) {
	return setJSONKeepTTL(ctx, key, value, true)
}

func setJSONKeepTTL(ctx context.Context, key string, value any, onlyIfExists bool) (bool, error) {
	client, err := GetRedis()
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("序列化缓存 JSON 失败: %w", err)
	}

	args := redis.SetArgs{KeepTTL: true}
	if onlyIfExists {
		args.Mode = "XX"
	}
	if err := client.SetArgs(ctx, key, payload, args).Err(); err != nil {
		return false, fmt.Errorf("刷新缓存失败: %w", err)
	}

	if !onlyIfExists {
		return true, nil
	}
	n, cerr := client.Exists(ctx, key).Result()
	if cerr != nil {
		return false, fmt.Errorf("确认缓存写入结果失败: %w", cerr)
	}
	return n > 0, nil
}

func TTL(ctx context.Context, key string) (time.Duration, error) {
	client, err := GetRedis()
	if err != nil {
		return 0, err
	}
	return client.TTL(ctx, key).Result()
}

func Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	client, err := GetRedis()
	if err != nil {
		return err
	}
	if err := client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("删除缓存失败: %w", err)
	}
	return nil
}

func GetJSON[T any](ctx context.Context, key string) (T, error) {
	var zero T

	client, err := GetRedis()
	if err != nil {
		return zero, err
	}

	payload, err := client.Get(ctx, key).Bytes()
	if err != nil {
		return zero, err
	}

	var result T
	if err := json.Unmarshal(payload, &result); err != nil {
		return zero, fmt.Errorf("反序列化缓存 JSON 失败: %w", err)
	}
	return result, nil
}

func RememberJSON[T any](
	ctx context.Context,
	key string,
	ttl time.Duration,
	jitter time.Duration,
	loader func(context.Context) (T, error),
) (T, error) {
	var zero T

	result, err := GetJSON[T](ctx, key)
	if err == nil {
		return result, nil
	}
	if err != redis.Nil {
		return zero, err
	}

	value, loadErr, _ := loadGroup.Do(key, func() (any, error) {
		loaded, err := loader(ctx)
		if err != nil {
			return zero, err
		}
		cacheTTL := addTTLJitter(ttl, jitter)
		if err := SetJSON(ctx, key, loaded, cacheTTL); err != nil {
			return zero, err
		}
		return loaded, nil
	})
	if loadErr != nil {
		return zero, loadErr
	}

	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("缓存加载结果类型断言失败")
	}
	return typed, nil
}

func IsInited() bool {
	return defaultProvider.IsInited()
}

func Ready() error {
	if !IsInited() {
		return fmt.Errorf("缓存组件未初始化")
	}
	return nil
}

func Close() error {
	return defaultProvider.Close()
}

func addTTLJitter(ttl time.Duration, jitter time.Duration) time.Duration {
	if ttl <= 0 || jitter <= 0 {
		return ttl
	}
	extra := time.Duration(rand.Int63n(int64(jitter)))
	return ttl + extra
}
