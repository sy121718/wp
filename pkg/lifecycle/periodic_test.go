package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// 首轮立即执行：调度不能写成「先等一个间隔」。
func TestPeriodicTaskRunsFirstRoundImmediately(t *testing.T) {
	calls := make(chan struct{}, 8)
	ctx, cancel := context.WithCancel(context.Background())
	task := PeriodicTask{Interval: time.Hour, Round: func(context.Context) { calls <- struct{}{} }}
	stop := task.Start(ctx)

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("首轮没有立即执行")
	}
	select {
	case <-calls:
		t.Fatal("间隔 1 小时时启动后 200ms 内不该有第二轮")
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	stop()
}

// 间隔真正生效。
func TestPeriodicTaskIntervalDrives(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	task := PeriodicTask{Interval: 10 * time.Millisecond, Round: func(context.Context) { calls.Add(1) }}
	stop := task.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()
	stop()
	if n := calls.Load(); n < 3 {
		t.Fatalf("10ms 间隔在 150ms 内至少应驱动 3 次，实际 %d", n)
	}
}

// 单轮 panic 被收敛，后续轮次照常。
func TestPeriodicTaskPanicDoesNotKillLoop(t *testing.T) {
	var calls, panics atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	task := PeriodicTask{
		Interval: 10 * time.Millisecond,
		Round: func(context.Context) {
			if calls.Add(1) == 1 {
				panic("boom")
			}
		},
		OnPanic: func(any) { panics.Add(1) },
	}
	stop := task.Start(ctx)
	time.Sleep(120 * time.Millisecond)
	cancel()
	stop()
	if calls.Load() < 3 {
		t.Fatalf("panic 后循环应继续，实际 %d 轮", calls.Load())
	}
	if panics.Load() != 1 {
		t.Fatalf("panic 应被精确收敛一次，实际 %d", panics.Load())
	}
}

// 取消后在途一轮必须先结束，Stop 才返回 —— 否则关闭数据库会与任务并发。
func TestPeriodicTaskStopWaitsInFlightRound(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	first.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	task := PeriodicTask{
		Interval: time.Hour,
		Round: func(context.Context) {
			if first.CompareAndSwap(true, false) {
				close(started)
				<-release
			}
		},
	}
	stop := task.Start(ctx)
	<-started
	cancel()

	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("在途一轮未结束时 Stop 就返回了")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("在途一轮结束后 Stop 仍未返回")
	}
}

// 单轮超时从父 ctx 派生，且不因超时取消父 ctx。
func TestPeriodicTaskRoundTimeoutDerivesFromParent(t *testing.T) {
	var sawDeadline atomic.Bool
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	task := PeriodicTask{
		Interval: 0,
		Timeout:  20 * time.Millisecond,
		Round: func(roundCtx context.Context) {
			if _, ok := roundCtx.Deadline(); ok {
				sawDeadline.Store(true)
			}
			close(started)
			<-roundCtx.Done()
		},
	}
	stop := task.Start(ctx)
	// 先等首轮真正进入，再取消父 ctx：启动前就取消的修复不让它发起新轮，
	// 那样断言到的只是「没跑」，不是超时派生。
	<-started
	cancel()
	stop()
	if !sawDeadline.Load() {
		t.Fatal("每轮 ctx 应从父 ctx 派生并带超时")
	}
	if ctx.Err() == nil {
		t.Fatal("单轮超时不应取消父 ctx")
	}
}
