package membershipservice

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// 日结首轮立即执行，间隔生效。
func TestRecalcLoopRunsRounds(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	stop := startRecalcLoop(ctx, 10*time.Millisecond, func(context.Context) { calls.Add(1) })
	time.Sleep(120 * time.Millisecond)
	cancel()
	stop()
	if n := calls.Load(); n < 2 {
		t.Fatalf("10ms 间隔在 120ms 内至少应驱动 2 轮，实际 %d", n)
	}
}

// 父 ctx 取消后不得再启动新轮：取消发生在首轮进行中，
// 首轮结束前不能再进第二轮。
func TestRecalcLoopStopsStartingNewRoundsAfterCancel(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	stop := startRecalcLoop(ctx, time.Millisecond, func(context.Context) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
	})
	<-started
	cancel()
	// 取消后释放首轮：循环应直接退出，不因 ticker 就绪再进一轮。
	close(release)
	stop()
	if n := calls.Load(); n != 1 {
		t.Fatalf("取消后不应再启动新轮，实际跑了 %d 轮", n)
	}
}

// 关键出口：先 cancel 再 stop 时，stop 必须等在途一轮真正结束 ——
// 不能因为 ctx 已取消就提前返回（那会让调用方在任务未退出时关闭数据库）。
func TestRecalcStopWaitsInFlightRoundAfterCancel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	stop := startRecalcLoop(ctx, time.Hour, func(context.Context) {
		close(started)
		<-release
	})
	<-started

	// 模拟正常关闭流程：先取消父 ctx，再调用 stop。
	cancel()
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("ctx 已取消但在途一轮未结束时 stop 就返回了 —— 数据库会在任务运行中被关闭")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("在途一轮结束后 stop 仍未返回")
	}
}

// 重复 stop 幂等：第二次调用不应重复等待或阻塞。
func TestRecalcStopIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := startRecalcLoop(ctx, time.Hour, func(context.Context) {})
	cancel()
	stop()
	stop()
}
