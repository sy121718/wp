package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// 启动前父 ctx 已取消：不得发起首轮。
func TestPeriodicTaskDoesNotStartRoundWhenParentCancelled(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stop := PeriodicTask{Interval: time.Hour, Round: func(context.Context) { calls.Add(1) }}.Start(ctx)
	stop()
	if n := calls.Load(); n != 0 {
		t.Fatalf("父 ctx 已取消时不应发起首轮，实际 %d 轮", n)
	}
}

// 间隔驱动与取消同时就绪时，取消优先：取消后不再启动新轮。
//
// 这里用很小的间隔放大「ticker 已就绪」的几率；取消发生在首轮内部，
// 恢复后循环必须直接退出而不是再跑一轮。
func TestPeriodicTaskCancelStopsNewRounds(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		var calls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		stop := PeriodicTask{Interval: time.Nanosecond, Round: func(context.Context) {
			if calls.Add(1) == 1 {
				close(started)
				<-release
			}
		}}.Start(ctx)
		<-started
		cancel()
		close(release)
		stop()
		if n := calls.Load(); n != 1 {
			t.Fatalf("第 %d 次：取消后不应再启动新轮，实际跑了 %d 轮", attempt+1, n)
		}
	}
}
