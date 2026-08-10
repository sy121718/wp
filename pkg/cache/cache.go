// Package cache 提供缓存组件根入口和高层调用门面。
//
// 设计目的：
// - 对上层隐藏具体缓存 provider
// - 统一缓存组件的 Init/Ready/Close 生命周期
// - 提供常用的 JSON 缓存 helper，减少业务层直接手写序列化和回源逻辑
package cache

import (
	"context"
	"encoding/json"
	"fmt"
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

// Init 初始化缓存组件。
//
// 说明：
// - 当前默认 provider 为 Redis
// - 调用后才允许 GetRedis / SetJSON / GetJSON / RememberJSON 等高层入口工作
// - v 中的 redis.provider 用于切换 provider，未配置时默认走 redis
// Init 初始化 Redis 缓存组件。
func Init(v *viper.Viper) error {
	provider, err := buildProvider(v)
	if err != nil {
		return err
	}
	defaultProvider = provider
	return defaultProvider.Init(v)
}

// GetRedis 返回底层 Redis 客户端。
//
// 适用场景：
// - 需要执行高层 helper 未覆盖的 Redis 操作
// - 框架层能力之外的特殊命令
//
// 不建议：
// - 业务代码优先直接操作底层客户端
// - 能用 SetJSON / GetJSON / RememberJSON 时，优先使用高层入口
// GetRedis 获取 Redis 客户端实例。
func GetRedis() (redis.UniversalClient, error) {
	return defaultProvider.Client()
}

// SetJSON 将任意结构体序列化为 JSON 后写入缓存。
//
// 参数说明：
// - ctx: 上下文，用于超时和取消控制
// - key: 缓存 key
// - value: 任意可 JSON 序列化的值
// - ttl: 缓存过期时间
//
// 使用示例：
// - `cache.SetJSON(ctx, "user:1", userDTO, time.Minute)`
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

// GetJSON 从缓存中读取 JSON 并反序列化为指定类型。
//
// 参数说明：
// - ctx: 上下文
// - key: 缓存 key
//
// 返回值说明：
// - 返回指定泛型类型的值
// - 缓存不存在时会返回 redis.Nil
//
// 使用示例：
// - `user, err := cache.GetJSON[UserDTO](ctx, "user:1")`
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

// RememberJSON 优先从缓存读取 JSON，缓存缺失时通过 loader 回源加载，并使用 singleflight 合并并发请求。
//
// jitter 表示在 ttl 基础上的随机抖动上限，用于降低同一批 key 同时过期带来的压力。
//
// 参数说明：
// - ctx: 上下文
// - key: 缓存 key
// - ttl: 基础缓存时长
// - jitter: 随机抖动上限；为 0 表示不加抖动
// - loader: 缓存未命中时的回源函数
//
// 设计收益：
// - 自动处理缓存未命中回源
// - 使用 singleflight 合并并发请求，降低击穿风险
// - 支持 TTL 抖动，降低同批 key 同时过期带来的雪崩风险
//
// 使用示例：
// - `value, err := cache.RememberJSON(ctx, "user:1", time.Minute, 5*time.Second, loader)`
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

// IsInited 判断缓存组件是否已初始化。
// IsInited 检查 Redis 是否已初始化。
func IsInited() bool {
	return defaultProvider.IsInited()
}

// Ready 检查缓存组件是否已达到可用状态。
// Ready 检查缓存组件是否已初始化。
func Ready() error {
	if !IsInited() {
		return fmt.Errorf("缓存组件未初始化")
	}
	return nil
}

// Close 关闭缓存组件并释放底层连接。
// Close 关闭 Redis 连接。
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
