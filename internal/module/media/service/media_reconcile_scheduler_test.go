package mediaservice

// media_reconcile_scheduler_test.go — 调度循环的驱动语义（不连库）。
//
// 为什么测这里：目标方法（ReconcileStorage / ReplayVariantBackfill）要数据库与文件系统，
// 模块内单测不碰这些依赖；而**调度语义**才是本次新增的东西 ——
// 首跑到底等不等一个间隔、注入的间隔有没有真的被用上、单轮 panic 会不会把进程带走。
// 这三件事错了的表现都是「看起来在跑、其实什么都没做」，没有任何编译期或运行期报错。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestMediaReconcileLoopFirstRunImmediately 首跑不等间隔。
//
// 注入一个远大于用例时长的间隔，断言动作仍然立刻发生（样板语义：先跑一次再等 ticker）。
// 若把顺序写成「先等一个间隔」，进程启动后 24 小时内不会有任何对账与补偿。
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

// TestMediaReconcileLoopIntervalDrives 可注入间隔真的生效。
//
// 毫秒级间隔必须驱动出多次执行；否则「可注入间隔」只是签名好看，
// 用例里的注入值并没有真正控制循环。
func TestMediaReconcileLoopIntervalDrives(t *testing.T) {
	var calls atomic.Int32
	stop := runMediaReconcileLoop(func() { calls.Add(1) }, 10*time.Millisecond)
	defer stop()

	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n < 3 {
		t.Fatalf("10ms 间隔在 150ms 内至少应驱动 3 次（含首跑），实际 %d 次 —— 注入的间隔没有被真正使用？", n)
	}
}

// TestMediaReconcileLoopPanicDoesNotKillLoop 单轮动作 panic 不能带走进程，也不能让循环停下。
//
// goroutine 里未被 recover 的 panic 会直接终止整个进程（Go 无法从别的 goroutine 恢复它），
// 巡检任务没有这种权力：一轮动作的 bug 只该让这一轮没有结论，下一个间隔照常再来。
func TestMediaReconcileLoopPanicDoesNotKillLoop(t *testing.T) {
	var calls atomic.Int32
	var panicked atomic.Bool
	stop := runMediaReconcileLoop(func() {
		calls.Add(1)
		// 只让**首跑** panic：后续轮次必须照常执行 —— 这证明的是「recover 之后循环还活着」，
		// 而不是「每一轮都在 panic、每一轮都被接住」（后者会让断言退化成只测了 recover 本身）。
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

// TestMediaReconcileRoundSurvivesNilDeps 依赖缺失（nil model）时一轮巡检不 panic。
//
// 这是「目标方法内部 panic」的真实路径：两个动作入口都会在 nil model 上解引用。
// 断言两件事：runMediaReconcileRound 不把 panic 抛出来，且两个动作**各自**被记为失败 ——
// 若只包了一层 recover，第一个动作的 panic 会让补偿重放这一轮被静默跳过。
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

// TestMediaReconcileSchedulerNilService 入口在 nil service 上直接返回（不起 goroutine、不 panic）。
func TestMediaReconcileSchedulerNilService(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil service 不该 panic：%v", r)
		}
	}()
	StartMediaReconcileSchedulerWithInterval(nil, time.Millisecond)
	StartMediaReconcileScheduler(nil)
}
