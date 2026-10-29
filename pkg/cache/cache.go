// Package cache 提供缓存组件根入口和高层调用门面。
//
// 设计目的：
// - 对上层隐藏具体缓存 provider
// - 统一缓存组件的 Init/Ready/Close 生命周期
// - 提供常用的 JSON 缓存 helper，减少业务层直接手写序列化和回源逻辑
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

// SetJSONKeepTTL 覆盖写入 JSON 值，并**保留 key 原有的 TTL**（redis.KeepTTL）。
//
// 用于「只改内容、不改有效期」的读改写场景 —— 典型是刷新会话的最后活跃时间：
// 用 SetJSON + 固定 TTL 重写会把「记住我」的长会话悄悄缩回默认时长，
// 而先读 TTL 再按剩余时长写回会引入一次额外的往返与竞态窗口。
func SetJSONKeepTTL(ctx context.Context, key string, value any) error {
	_, err := setJSONKeepTTL(ctx, key, value, false)
	return err
}

// SetJSONKeepTTLIfExists 仅当 key **仍然存在**时覆盖写入，返回是否写入成功。
//
// 存在的理由是一个真实的提权形态（SEC-03，2026-10-29 实测）：
// 「刷新最后活跃时间」是**读-改-写**，中间有一个窗口能被撤销插进来：
//
//	Touch 读到会话 → Revoke 删除会话 → Touch 用 KeepTTL 写回
//
// 而 Redis 的 `SET ... KEEPTTL` 在 key **不存在**时不会失败，它会把 key 当成
// 无过期时间写进去（实测 TTL = -1）。于是被撤销的会话不仅复活，还永不过期。
//
// 用 `XX`（只在 key 存在时设置）把这一步变成条件写：会话已被撤销时写入不生效，
// 返回 false 让调用方知道「这次 Touch 落空了」—— 那是正常情况（用户正好退出），
// 不是错误。
func SetJSONKeepTTLIfExists(ctx context.Context, key string, value any) (bool, error) {
	return setJSONKeepTTL(ctx, key, value, true)
}

// setJSONKeepTTL 两者的共同实现（onlyIfExists 决定是否带 XX）。
func setJSONKeepTTL(ctx context.Context, key string, value any, onlyIfExists bool) (bool, error) {
	client, err := GetRedis()
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("序列化缓存 JSON 失败: %w", err)
	}
	// XX + KeepTTL：只在 key 已存在时改内容、且不动过期时间。
	//
	// 为什么用 `SetArgs` 而不是 `Set(..., redis.KeepTTL)`：后者在 key 不存在时会
	// **创建**它（无过期时间），正是要消灭的那条路。`XX` 让它变成条件写。
	args := redis.SetArgs{KeepTTL: true}
	if onlyIfExists {
		args.Mode = "XX"
	}
	if err := client.SetArgs(ctx, key, payload, args).Err(); err != nil {
		return false, fmt.Errorf("刷新缓存失败: %w", err)
	}
	// `SET ... XX` 在 key 不存在时不报错但也不写入，Redis 不回「写没写」——
	// 所以要**再确认一次存在性**。这一次往返是必须的：调用方要区分
	// 「更新了」与「会话已被撤销」。
	if !onlyIfExists {
		return true, nil
	}
	n, cerr := client.Exists(ctx, key).Result()
	if cerr != nil {
		return false, fmt.Errorf("确认缓存写入结果失败: %w", cerr)
	}
	return n > 0, nil
}

// TTL 返回 key 的剩余存活时间（语义与 Redis TTL 一致：-2 = key 不存在，-1 = 无过期时间）。
//
// 用途：需要「改写值但不改变有效期」的场景（例如回填会话内容时保留登录时设置的
// rememberMe 有效期），避免用固定 TTL 重写把长会话悄悄缩短。
func TTL(ctx context.Context, key string) (time.Duration, error) {
	client, err := GetRedis()
	if err != nil {
		return 0, err
	}
	return client.TTL(ctx, key).Result()
}

// Delete 删除一个或多个缓存 key（不存在的 key 不视为错误）。
//
// 与 SetJSON 对称的基础操作。刻意不返回「删掉了几条」：调用方要的是
// 「这个 key 之后不再存在」，而不是「这次删掉了几条」——后者会让
// 「本来就没这条」变成一个需要被处理的分支，而它并不是错误。
//
// 使用示例：
// - `cache.Delete(ctx, "userauth:sess:abc")`
// - `cache.Delete(ctx, key1, key2)`
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
