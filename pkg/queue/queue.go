package queue

import (
	"fmt"
	"strings"
	"time"

	queueprovider "go_wp/pkg/queue/provider"

	"github.com/spf13/viper"
)

type Handler = queueprovider.Handler
type Option = queueprovider.Option

type Task struct {
	taskType string
	options  []Option
}

func WithQueue(name string) Option {
	return queueprovider.WithQueue(name)
}

func WithMaxRetry(n int) Option {
	return queueprovider.WithMaxRetry(n)
}

func WithTimeout(timeout time.Duration) Option {
	return queueprovider.WithTimeout(timeout)
}

func WithDeadline(deadline time.Time) Option {
	return queueprovider.WithDeadline(deadline)
}

func WithUnique(ttl time.Duration) Option {
	return queueprovider.WithUnique(ttl)
}

func WithTaskID(id string) Option {
	return queueprovider.WithTaskID(id)
}

func WithRetention(retention time.Duration) Option {
	return queueprovider.WithRetention(retention)
}

func NewTask(taskType string, options ...Option) Task {
	return Task{
		taskType: taskType,
		options:  append([]Option(nil), options...),
	}
}

func Start() error {
	return defaultProvider.Start()
}

func Shutdown() error {
	return defaultProvider.Shutdown()
}

func Close() error {
	return Shutdown()
}

func IsInited() bool {
	return defaultProvider.IsInited()
}

func Ready() error {
	if !IsInited() {
		return fmt.Errorf("任务队列组件未初始化")
	}
	return nil
}

func Enqueue(taskType string, payload any, opts ...Option) error {
	return defaultProvider.Enqueue(taskType, payload, opts...)
}

func EnqueueIn(taskType string, delay time.Duration, payload any, opts ...Option) error {
	return defaultProvider.EnqueueIn(taskType, delay, payload, opts...)
}

func EnqueueAt(taskType string, at time.Time, payload any, opts ...Option) error {
	return defaultProvider.EnqueueAt(taskType, at, payload, opts...)
}

func buildProvider(v *viper.Viper) (queueprovider.Provider, error) {
	if v == nil {
		return nil, fmt.Errorf("queue 配置不能为空")
	}

	providerName := strings.TrimSpace(strings.ToLower(v.GetString("queue.provider")))
	if providerName == "" {
		return nil, fmt.Errorf("queue.provider 不能为空")
	}

	switch providerName {
	case "asynq":
		return queueprovider.NewAsynqProvider(), nil
	default:
		return nil, fmt.Errorf("不支持的队列 provider: %s", providerName)
	}
}

var defaultProvider queueprovider.Provider = queueprovider.NewAsynqProvider()
var registrations = map[string]Handler{}

func Init(v *viper.Viper) error {
	provider, err := buildProvider(v)
	if err != nil {
		return err
	}

	for taskType, handler := range registrations {
		provider.Register(taskType, handler)
	}

	defaultProvider = provider
	if err := defaultProvider.Init(v); err != nil {
		return err
	}
	if v != nil && v.GetBool("queue.run_worker") {
		if err := defaultProvider.Start(); err != nil {
			return err
		}
	}
	return nil
}

func Register(taskType string, handler Handler) {
	registrations[taskType] = handler
	defaultProvider.Register(taskType, handler)
}

func (t Task) Enqueue(payload any, options ...Option) error {
	return Enqueue(t.taskType, payload, append(t.options, options...)...)
}

func (t Task) EnqueueIn(delay time.Duration, payload any, options ...Option) error {
	return EnqueueIn(t.taskType, delay, payload, append(t.options, options...)...)
}

func (t Task) EnqueueAt(at time.Time, payload any, options ...Option) error {
	return EnqueueAt(t.taskType, at, payload, append(t.options, options...)...)
}
