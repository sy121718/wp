package mediaservice

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMediaReconcileLoopFirstRunImmediately(t *testing.T) {
	calls := make(chan struct{}, 8)
	stop := runMediaReconcileLoop(func() { calls <- struct{}{} }, time.Hour)
	defer stop()

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("首跑没有在启动后立即发生（调度被写成「先等一个间隔」？那启动后 24 小时内不会有任何巡检）")
	}
	select {
	case <-calls:
		t.Fatal("间隔为 1 小时时，启动后 200ms 内不该出现第二次调用")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestMediaReconcileLoopIntervalDrives(t *testing.T) {
	var calls atomic.Int32
	stop := runMediaReconcileLoop(func() { calls.Add(1) }, 10*time.Millisecond)
	defer stop()

	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n < 3 {
		t.Fatalf("10ms 间隔在 150ms 内至少应驱动 3 次（含首跑），实际 %d 次 —— 注入的间隔没有被真正使用？", n)
	}
}

func TestMediaReconcileLoopPanicDoesNotKillLoop(t *testing.T) {
	var calls atomic.Int32
	var panicked atomic.Bool
	stop := runMediaReconcileLoop(func() {
		calls.Add(1)

		if panicked.CompareAndSwap(false, true) {
			panic("模拟目标方法内部 panic")
		}
	}, 10*time.Millisecond)
	defer stop()

	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n < 3 {
		t.Fatalf("单轮 panic 后循环必须继续按间隔驱动（recover 没接住？），实际只跑了 %d 次", n)
	}
}

func TestMediaReconcileRoundSurvivesNilDeps(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("一轮巡检不该把 panic 抛出来（调度 goroutine 里 panic 会终止整个进程）：%v", r)
		}
	}()
	res := runMediaReconcileRound(context.Background(), &Service{})
	if res.reconcileErr == nil {
		t.Error("依赖缺失时只读对账应记为失败（panic 应被收敛成该动作的错误）")
	}
	if res.backfillErr == nil {
		t.Error("依赖缺失时变体补偿应记为失败 —— 一个动作 panic 不该让另一个动作被跳过")
	}
}

func TestMediaReconcileSchedulerNilService(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil service 不该 panic：%v", r)
		}
	}()
	StartMediaReconcileSchedulerWithInterval(nil, time.Millisecond)
	StartMediaReconcileScheduler(nil)
}
