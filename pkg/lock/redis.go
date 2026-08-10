package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// RedisLocker Redis 分布式锁实现。
//
// 适用场景：
// - 跨实例互斥
// - 定时任务单实例执行
// - 重复消费防护
type RedisLocker struct {
	client redis.UniversalClient
	prefix string
}

type redisLease struct {
	client redis.UniversalClient
	key    string
	token  string
}

// NewRedis 创建 Redis 分布式锁实现。
func NewRedis(client redis.UniversalClient, prefix string) *RedisLocker {
	return &RedisLocker{
		client: client,
		prefix: prefix,
	}
}

// Acquire 尝试获取 Redis 锁。
func (l *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (Lease, error) {
	if l.client == nil {
		return nil, fmt.Errorf("redis 锁客户端不能为空")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("锁 TTL 必须大于 0")
	}

	token, err := randomToken()
	if err != nil {
		return nil, err
	}

	fullKey := l.prefix + key
	ok, err := l.client.SetNX(ctx, fullKey, token, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("锁已被占用: %s", key)
	}

	return &redisLease{
		client: l.client,
		key:    fullKey,
		token:  token,
	}, nil
}

func (l *redisLease) Key() string {
	return l.key
}

func (l *redisLease) Release(ctx context.Context) error {
	_, err := releaseScript.Run(ctx, l.client, []string{l.key}, l.token).Result()
	return err
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
