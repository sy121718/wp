package queueprovider

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/spf13/viper"
)

// Handler 任务处理器签名
type Handler func(context.Context, []byte) error

// Option 任务入队选项
type Option func(*providerOptionBag) error

type providerOptionBag struct {
	asynqOptions []asynq.Option
}

// Provider 任务队列实现接口
type Provider interface {
	Init(v *viper.Viper) error
	IsInited() bool
	Start() error
	Shutdown() error
	Enqueue(taskType string, payload any, opts ...Option) error
	EnqueueIn(taskType string, delay time.Duration, payload any, opts ...Option) error
	EnqueueAt(taskType string, at time.Time, payload any, opts ...Option) error
	Register(taskType string, handler Handler)
}
