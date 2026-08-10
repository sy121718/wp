package cacheprovider

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

const (
	defaultRedisHost     = "127.0.0.1"
	defaultRedisPort     = 6379
	defaultRedisPassword = ""
	defaultRedisDB       = 0
)

// Config Redis 配置。
type Config struct {
	Host     string   `mapstructure:"host"`
	Port     int      `mapstructure:"port"`
	Password string   `mapstructure:"password"`
	DB       int      `mapstructure:"db"`
	Addrs    []string `mapstructure:"addrs"`
}

type redisProvider struct {
	rdb    redis.UniversalClient
	mu     sync.RWMutex
	inited bool
}

func getDefaultConfig() Config {
	return Config{
		Host:     defaultRedisHost,
		Port:     defaultRedisPort,
		Password: defaultRedisPassword,
		DB:       defaultRedisDB,
	}
}

func normalizeConfig(cfg Config) Config {
	defaultCfg := getDefaultConfig()
	if cfg.Host == "" {
		cfg.Host = defaultCfg.Host
	}
	if cfg.Port == 0 {
		cfg.Port = defaultCfg.Port
	}
	if len(cfg.Addrs) == 0 {
		cfg.Addrs = []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)}
	}
	return cfg
}

func (p *redisProvider) initRedis(v *viper.Viper) (redis.UniversalClient, error) {
	cfg := getDefaultConfig()
	if v != nil {
		if err := v.UnmarshalKey("redis", &cfg); err != nil {
			log.Printf("解析 redis 配置失败，使用默认配置: %v", err)
			cfg = getDefaultConfig()
		}
	}
	cfg = normalizeConfig(cfg)

	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    cfg.Addrs,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis 连接失败: %w", err)
	}

	log.Println("redis 初始化成功")
	return client, nil
}

func (p *redisProvider) Init(v *viper.Viper) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.inited {
		return nil
	}

	client, err := p.initRedis(v)
	if err != nil {
		return fmt.Errorf("redis 初始化失败: %w", err)
	}

	p.rdb = client
	p.inited = true
	return nil
}

func (p *redisProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.rdb == nil {
		return nil
	}

	if err := p.rdb.Close(); err != nil {
		return err
	}

	p.rdb = nil
	p.inited = false
	return nil
}

func (p *redisProvider) Client() (redis.UniversalClient, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.rdb == nil {
		return nil, fmt.Errorf("redis 未初始化，请先调用 cache.Init()")
	}
	return p.rdb, nil
}

func (p *redisProvider) IsInited() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.inited && p.rdb != nil
}

// NewRedisProvider 创建 Redis 实现。
func NewRedisProvider() Provider {
	return &redisProvider{}
}
