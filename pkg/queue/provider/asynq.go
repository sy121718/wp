// Package queueprovider /*
package queueprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/spf13/viper"
)

type asynqProvider struct {
	client        *asynq.Client
	server        *asynq.Server
	serveMux      *asynq.ServeMux
	registrations []handlerRegistration
	mu            sync.RWMutex
}

type handlerRegistration struct {
	taskType string
	handler  Handler
}

type Config struct {
	Concurrency int            `mapstructure:"concurrency"`
	Queues      map[string]int `mapstructure:"queues"`
	Redis       struct {
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		Password string `mapstructure:"password"`
		DB       int    `mapstructure:"db"`
	} `mapstructure:"redis"`
}

const (
	defaultQueueConcurrency = 10
	defaultQueueRedisHost   = "127.0.0.1"
	defaultQueueRedisPort   = 6379
	defaultQueueRedisPass   = ""
	defaultQueueRedisDB     = 0
)

func getDefaultConfig() Config {
	cfg := Config{
		Concurrency: defaultQueueConcurrency,
		Queues: map[string]int{
			"critical": 6,
			"default":  3,
			"low":      1,
		},
	}
	cfg.Redis.Host = defaultQueueRedisHost
	cfg.Redis.Port = defaultQueueRedisPort
	cfg.Redis.Password = defaultQueueRedisPass
	cfg.Redis.DB = defaultQueueRedisDB
	return cfg
}

func parseConfig(v *viper.Viper) Config {
	cfg := getDefaultConfig()
	if v == nil {
		return cfg
	}

	if err := v.UnmarshalKey("queue", &cfg); err != nil {
		return getDefaultConfig()
	}

	defaultCfg := getDefaultConfig()
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultCfg.Concurrency
	}
	if len(cfg.Queues) == 0 {
		cfg.Queues = defaultCfg.Queues
	}
	if cfg.Redis.Host == "" {
		cfg.Redis.Host = v.GetString("redis.host")
	}
	if cfg.Redis.Host == "" {
		cfg.Redis.Host = defaultCfg.Redis.Host
	}
	if cfg.Redis.Port == 0 {
		cfg.Redis.Port = v.GetInt("redis.port")
	}
	if cfg.Redis.Port == 0 {
		cfg.Redis.Port = defaultCfg.Redis.Port
	}
	if cfg.Redis.Password == "" {
		cfg.Redis.Password = v.GetString("redis.password")
	}
	if cfg.Redis.DB == 0 {
		cfg.Redis.DB = v.GetInt("redis.db")
	}
	return cfg
}

func applyOptions(opts []Option) ([]asynq.Option, error) {
	if len(opts) == 0 {
		return nil, nil
	}

	cfg := providerOptionBag{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	return cfg.asynqOptions, nil
}

func (p *asynqProvider) ensureClientReady() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.client == nil {
		return errors.New("任务队列客户端未初始化")
	}
	return nil
}

func (p *asynqProvider) ensureServerReady() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.server == nil || p.serveMux == nil {
		return errors.New("任务队列服务端未初始化")
	}
	return nil
}

func (p *asynqProvider) Init(v *viper.Viper) error {
	cfg := parseConfig(v)

	p.mu.Lock()
	if p.client != nil && p.server != nil && p.serveMux != nil {
		p.mu.Unlock()
		return nil
	}

	redisOpt := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	p.client = asynq.NewClient(redisOpt)
	p.serveMux = asynq.NewServeMux()
	p.server = asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: cfg.Concurrency,
			Queues:      cfg.Queues,
		},
	)

	registered := append([]handlerRegistration{}, p.registrations...)
	mux := p.serveMux
	p.mu.Unlock()

	for _, item := range registered {
		taskType := item.taskType
		handler := item.handler
		mux.HandleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
			return handler(ctx, task.Payload())
		})
	}

	return nil
}

func (p *asynqProvider) Start() error {
	if err := p.ensureServerReady(); err != nil {
		return err
	}

	p.mu.RLock()
	s := p.server
	m := p.serveMux
	p.mu.RUnlock()

	go func() {
		if err := s.Start(m); err != nil {
			fmt.Printf("队列 worker 异常退出: %v\n", err)
		}
	}()
	return nil
}

func (p *asynqProvider) IsInited() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client != nil && p.server != nil && p.serveMux != nil
}

func (p *asynqProvider) Shutdown() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.server != nil {
		p.server.Shutdown()
	}
	if p.client != nil {
		p.client.Close()
	}

	p.client = nil
	p.server = nil
	p.serveMux = nil
	return nil
}

func (p *asynqProvider) Enqueue(taskType string, payload any, opts ...Option) error {
	if err := p.ensureClientReady(); err != nil {
		return err
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	asynqOpts, err := applyOptions(opts)
	if err != nil {
		return err
	}

	p.mu.RLock()
	c := p.client
	p.mu.RUnlock()

	task := asynq.NewTask(taskType, data)
	_, err = c.Enqueue(task, asynqOpts...)
	return err
}

func (p *asynqProvider) EnqueueIn(taskType string, delay time.Duration, payload any, opts ...Option) error {
	if delay <= 0 {
		return fmt.Errorf("延迟时间必须大于 0")
	}

	opts = append(opts, func(cfg *providerOptionBag) error {
		cfg.asynqOptions = append(cfg.asynqOptions, asynq.ProcessIn(delay))
		return nil
	})
	return p.Enqueue(taskType, payload, opts...)
}

func (p *asynqProvider) EnqueueAt(taskType string, at time.Time, payload any, opts ...Option) error {
	if at.IsZero() {
		return fmt.Errorf("执行时间不能为空")
	}

	opts = append(opts, func(cfg *providerOptionBag) error {
		cfg.asynqOptions = append(cfg.asynqOptions, asynq.ProcessAt(at))
		return nil
	})
	return p.Enqueue(taskType, payload, opts...)
}

func (p *asynqProvider) Register(taskType string, handler Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.registrations = append(p.registrations, handlerRegistration{
		taskType: taskType,
		handler:  handler,
	})

	if p.serveMux != nil {
		p.serveMux.HandleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
			return handler(ctx, task.Payload())
		})
	}
}

func WithQueue(name string) Option {
	return func(opts *providerOptionBag) error {
		if name == "" {
			return fmt.Errorf("队列名称不能为空")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.Queue(name))
		return nil
	}
}

func WithMaxRetry(n int) Option {
	return func(opts *providerOptionBag) error {
		if n < 0 {
			return fmt.Errorf("最大重试次数不能小于 0")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.MaxRetry(n))
		return nil
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(opts *providerOptionBag) error {
		if timeout <= 0 {
			return fmt.Errorf("超时时间必须大于 0")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.Timeout(timeout))
		return nil
	}
}

func WithDeadline(deadline time.Time) Option {
	return func(opts *providerOptionBag) error {
		if deadline.IsZero() {
			return fmt.Errorf("截止时间不能为空")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.Deadline(deadline))
		return nil
	}
}

func WithUnique(ttl time.Duration) Option {
	return func(opts *providerOptionBag) error {
		if ttl <= 0 {
			return fmt.Errorf("唯一锁时间必须大于 0")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.Unique(ttl))
		return nil
	}
}

func WithTaskID(id string) Option {
	return func(opts *providerOptionBag) error {
		if id == "" {
			return fmt.Errorf("任务 ID 不能为空")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.TaskID(id))
		return nil
	}
}

func WithRetention(retention time.Duration) Option {
	return func(opts *providerOptionBag) error {
		if retention <= 0 {
			return fmt.Errorf("保留时长必须大于 0")
		}
		opts.asynqOptions = append(opts.asynqOptions, asynq.Retention(retention))
		return nil
	}
}

// NewAsynqProvider 创建 Asynq 实现
func NewAsynqProvider() Provider {
	return &asynqProvider{}
}
