// Package queue 提供任务队列组件根入口和高层任务门面。
//
// 设计目标：
// - 对上层隐藏具体队列 provider
// - 统一队列组件生命周期：Init / Ready / Close
// - 提供 Task 门面，减少业务层反复传 taskType
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

// Task 表示一个具备固定任务类型和默认选项的高层任务门面。
//
// 使用场景：
// - 业务层希望固定某个 taskType
// - 业务层希望给一组任务附加默认 Option
//
// 示例：
// - `emailTask := queue.NewTask("email:send", queue.WithQueue("critical"))`
// - `err := emailTask.Enqueue(payload)`
type Task struct {
	taskType string
	options  []Option
}

// WithQueue 指定任务队列名称。
func WithQueue(name string) Option {
	return queueprovider.WithQueue(name)
}

// WithMaxRetry 指定任务最大重试次数。
func WithMaxRetry(n int) Option {
	return queueprovider.WithMaxRetry(n)
}

// WithTimeout 指定任务执行超时时间。
func WithTimeout(timeout time.Duration) Option {
	return queueprovider.WithTimeout(timeout)
}

// WithDeadline 指定任务执行截止时间。
func WithDeadline(deadline time.Time) Option {
	return queueprovider.WithDeadline(deadline)
}

// WithUnique 指定任务唯一锁时长。
func WithUnique(ttl time.Duration) Option {
	return queueprovider.WithUnique(ttl)
}

// WithTaskID 指定任务 ID。
func WithTaskID(id string) Option {
	return queueprovider.WithTaskID(id)
}

// WithRetention 指定任务结果保留时间。
func WithRetention(retention time.Duration) Option {
	return queueprovider.WithRetention(retention)
}

// NewTask 创建一个高层任务门面，用于减少业务侧反复手工传 taskType。
//
// 参数说明：
// - taskType: 任务类型
// - options: 默认任务选项
func NewTask(taskType string, options ...Option) Task {
	return Task{
		taskType: taskType,
		options:  append([]Option(nil), options...),
	}
}

// Start 启动队列 worker。
// Start 启动任务队列
func Start() error {
	return defaultProvider.Start()
}

// Shutdown 停止队列 worker 并关闭底层资源。
// Shutdown 关闭任务队列
func Shutdown() error {
	return defaultProvider.Shutdown()
}

// Close 作为统一组件协议入口，语义等同于 Shutdown。
// Close 关闭任务队列（统一组件协议入口）。
func Close() error {
	return Shutdown()
}

// IsInited 判断队列组件是否已初始化完成。
// IsInited 判断任务队列组件是否已完成初始化。
func IsInited() bool {
	return defaultProvider.IsInited()
}

// Ready 检查队列组件是否达到可用状态。
// Ready 检查任务队列组件是否可用。
func Ready() error {
	if !IsInited() {
		return fmt.Errorf("任务队列组件未初始化")
	}
	return nil
}

// Enqueue 立即入队一个任务。
//
// 参数说明：
// - taskType: 任务类型
// - payload: 任意可 JSON 序列化的数据
// - opts: 任务选项
func Enqueue(taskType string, payload any, opts ...Option) error {
	return defaultProvider.Enqueue(taskType, payload, opts...)
}

// EnqueueIn 延迟入队一个任务。
func EnqueueIn(taskType string, delay time.Duration, payload any, opts ...Option) error {
	return defaultProvider.EnqueueIn(taskType, delay, payload, opts...)
}

// EnqueueAt 在指定时间入队一个任务。
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

// Init 初始化任务队列
//
// 说明：
// - 会根据配置选择 provider
// - 会重放已经注册的 handler
// - 当 queue.run_worker=true 时，会在 Init 内直接启动 worker
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

// Register 注册任务处理器。
//
// 规则：
// - 同 taskType 重复注册时，以最后一次注册为准
// - 注册关系会在 Init 时重放到具体 provider
func Register(taskType string, handler Handler) {
	registrations[taskType] = handler
	defaultProvider.Register(taskType, handler)
}

// Enqueue 通过任务门面立即入队。
//
// 说明：
// - 会自动复用 Task 里保存的 taskType 和默认选项
func (t Task) Enqueue(payload any, options ...Option) error {
	return Enqueue(t.taskType, payload, append(t.options, options...)...)
}

// EnqueueIn 通过任务门面延迟入队。
func (t Task) EnqueueIn(delay time.Duration, payload any, options ...Option) error {
	return EnqueueIn(t.taskType, delay, payload, append(t.options, options...)...)
}

// EnqueueAt 通过任务门面在指定时间入队。
func (t Task) EnqueueAt(at time.Time, payload any, options ...Option) error {
	return EnqueueAt(t.taskType, at, payload, append(t.options, options...)...)
}
