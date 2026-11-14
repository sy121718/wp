package lifecycle

import (
	"context"
	"sync"
	"time"
)

// PeriodicTask 是一个可取消、可等待的周期执行任务。
//
// 它只提供「首轮立即执行 + 固定间隔 + 每轮超时 + panic 隔离 + 取消后等待在途一轮」
// 这一层机制；业务回调、间隔、超时与日志都留原属主。不与 Asynq 队列合并，
// 也不是 cron 平台。
type PeriodicTask struct {
	// Round 执行一轮（业务回调）。必须遵守传入的 ctx。
	Round func(ctx context.Context)
	// Interval 是两轮之间的间隔；<=0 时按 0 处理（只跑首轮，不重复）。
	Interval time.Duration
	// Timeout 是单轮超时；<=0 表示不额外设超时（沿用父 ctx）。
	Timeout time.Duration
	// OnPanic 收敛单轮 panic；nil 时静默吞掉（由调用方决定是否记录）。
	OnPanic func(recovered any)
}

// Start 在父 ctx 下启动任务，返回 Stop。
//
// 首轮立即执行；取消后不再启动新轮，Stop 等待在途一轮结束后才返回 ——
// 调用方必须在关闭数据库之前调用 Stop。
func (t PeriodicTask) Start(parent context.Context) (stop func()) {
	if t.Round == nil {
		return func() {}
	}
	if parent == nil {
		parent = context.Background()
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		run := func() {
			ctx := parent
			if t.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(parent, t.Timeout)
				defer cancel()
			}
			t.invoke(ctx)
		}
		// 启动时父 ctx 已取消：不再发起首轮（取消后不启动新轮）。
		if parent.Err() != nil {
			return
		}
		run()
		if t.Interval <= 0 {
			<-parent.Done()
			return
		}
		ticker := time.NewTicker(t.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-ticker.C:
				// select 在两个 case 同时就绪时随机挑选：取消已经发生时
				// 不能再靠 ticker 分支发起新轮。
				if parent.Err() != nil {
					return
				}
				run()
			}
		}
	}()
	return func() { once.Do(func() { <-done }) }
}

// invoke 执行一轮并隔离 panic：单轮 panic 不能终止进程或后续轮次。
func (t PeriodicTask) invoke(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil && t.OnPanic != nil {
			t.OnPanic(r)
		}
	}()
	t.Round(ctx)
}
