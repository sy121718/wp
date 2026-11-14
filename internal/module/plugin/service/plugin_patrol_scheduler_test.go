package pluginservice

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPluginPatrolLoopFirstRunImmediately(t *testing.T) {
	calls := make(chan struct{}, 8)
	stop, wait := runPluginPatrolLoop(context.Background(), func(context.Context) { calls <- struct{}{} }, time.Hour)
	defer func() { stop(); wait() }()

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("首跑没有在启动后立即发生（调度被写成「先等一个间隔」？）")
	}
	select {
	case <-calls:
		t.Fatal("间隔为 1 小时时，启动后 200ms 内不该出现第二次调用")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPluginPatrolLoopIntervalDrives(t *testing.T) {
	var calls atomic.Int32
	stop, wait := runPluginPatrolLoop(context.Background(), func(context.Context) { calls.Add(1) }, 10*time.Millisecond)
	defer func() { stop(); wait() }()

	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n < 3 {
		t.Fatalf("10ms 间隔在 150ms 内至少应驱动 3 次（含首跑），实际 %d 次 —— 注入的间隔没有被真正使用？", n)
	}
}

func TestPluginPatrolLoopPanicDoesNotKillLoop(t *testing.T) {
	var calls atomic.Int32
	var panicked atomic.Bool
	stop, wait := runPluginPatrolLoop(context.Background(), func(context.Context) {
		calls.Add(1)

		if panicked.CompareAndSwap(false, true) {
			panic("模拟目标方法内部 panic")
		}
	}, 10*time.Millisecond)
	defer func() { stop(); wait() }()

	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n < 3 {
		t.Fatalf("单轮 panic 后循环必须继续按间隔驱动（recover 没接住？），实际只跑了 %d 次", n)
	}
}

func TestPluginPatrolRoundSurvivesNilDeps(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("一轮巡检不该把 panic 抛出来（调度 goroutine 里 panic 会终止整个进程）：%v", r)
		}
	}()
	res := runPluginPatrolRound(context.Background(), &Service{})
	if res.err == nil {
		t.Error("依赖缺失时巡检应记为失败（panic 应被收敛成本轮错误，而不是逃出去）")
	}
}

func TestPluginPatrolRoundInconsistent(t *testing.T) {
	if n := (pluginPatrolRoundResult{}).inconsistent(); n != 0 {
		t.Fatalf("空结果的不一致数应为 0，实际 %d（会让每一轮都报「有不一致」）", n)
	}
	res := pluginPatrolRoundResult{orphanSchemas: 1, missingStorage: 2}
	if n := res.inconsistent(); n != 3 {
		t.Fatalf("四类计数之和应为 3，实际 %d", n)
	}
}

func TestPluginPatrolSchedulerNilService(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil service 不该 panic：%v", r)
		}
	}()
	StartPluginPatrolSchedulerWithInterval(context.Background(), nil, time.Millisecond)()
	StartPluginPatrolScheduler(context.Background(), nil)()
}

// stop 必须等到在途一轮结束（wait 返回）才允许关闭数据库。
func TestPluginPatrolLoopWaitsInFlightRound(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	stop, wait := runPluginPatrolLoop(context.Background(), func(context.Context) {
		select {
		case <-started:
			return
		default:
			close(started)
		}
		<-release
	}, time.Hour)

	<-started
	stop()
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
		t.Fatal("在途一轮尚未结束时 wait 就返回了 —— 关闭数据库会与巡检并发")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("在途一轮结束后 wait 仍未返回")
	}
}
