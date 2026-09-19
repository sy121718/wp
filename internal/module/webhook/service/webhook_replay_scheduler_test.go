package webhookservice

// webhook_replay_scheduler_test.go — 调度循环的驱动语义（不连库、不连队列）。
//
// 为什么测这里：目标方法 ReplayPendingDeliveries 要数据库与 Redis 队列，模块内单测不碰
// 这些依赖；而**调度语义**才是本次新增的东西 —— 首跑等不等间隔、注入的间隔有没有被用上、
// 单轮 panic 会不会把进程带走。三件事错了的表现都是「看起来在跑、pending 一直不清」，
// 没有任何编译期或运行期报错。

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestWebhookReplayIntervalExceedsMinAge 调度间隔必须显著大于陈旧阈值 replayMinAge。
//
// 这是**跨文件的不变量**：间隔是本文件的事实、陈旧阈值是 webhook_replay.go 的事实，
// 两者是同一份推理的两半（间隔若接近或小于 MinAge，每一轮都会对着同一批刚派发的
// pending 重投，退化成「把重复投递做成了常态」）。用测试而不是注释来守它 ——
// 注释会随 replayMinAge 改动而过时，测试不会：改小 replayMinAge 或改短间隔都会在这里失败。
func TestWebhookReplayIntervalExceedsMinAge(t *testing.T) {
	if webhookReplayInterval <= 2*replayMinAge {
		t.Fatalf("重放间隔 %s 必须显著大于 replayMinAge %s（否则每轮都会对刚派发的 pending 重投）",
			webhookReplayInterval, replayMinAge)
	}
}

// TestWebhookReplayLoopFirstRunImmediately 首跑不等间隔（样板语义：先跑一次再等 ticker）。
//
// 注入 1 小时（远大于用例时长）仍必须立刻跑一次；否则进程启动后一小时内，
// 「入队丢了」的投递不会得到任何重投。
func TestWebhookReplayLoopFirstRunImmediately(t *testing.T) {
	calls := make(chan struct{}, 8)
	stop := runWebhookReplayLoop(func() { calls <- struct{}{} }, time.Hour)
	defer stop()

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

// TestWebhookReplayLoopIntervalDrives 可注入间隔真的生效。
func TestWebhookReplayLoopIntervalDrives(t *testing.T) {
	var calls atomic.Int32
	stop := runWebhookReplayLoop(func() { calls.Add(1) }, 10*time.Millisecond)
	defer stop()

	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n < 3 {
		t.Fatalf("10ms 间隔在 150ms 内至少应驱动 3 次（含首跑），实际 %d 次 —— 注入的间隔没有被真正使用？", n)
	}
}

// TestWebhookReplayLoopPanicDoesNotKillLoop 单轮动作 panic 不能带走进程，也不能让循环停下。
func TestWebhookReplayLoopPanicDoesNotKillLoop(t *testing.T) {
	var calls atomic.Int32
	var panicked atomic.Bool
	stop := runWebhookReplayLoop(func() {
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

// TestWebhookReplayRoundSurvivesNilDeps 依赖缺失（nil model）时一轮重放不 panic，而是记为失败。
func TestWebhookReplayRoundSurvivesNilDeps(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("一轮重放不该把 panic 抛出来（调度 goroutine 里 panic 会终止整个进程）：%v", r)
		}
	}()
	res := runWebhookReplayRound(context.Background(), &Service{})
	if res.err == nil {
		t.Error("依赖缺失时重放应记为失败（panic 应被收敛成本轮错误，而不是逃出去）")
	}
}

// TestWebhookReplaySchedulerNilService 入口在 nil service 上直接返回（不起 goroutine、不 panic）。
func TestWebhookReplaySchedulerNilService(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil service 不该 panic：%v", r)
		}
	}()
	StartWebhookReplaySchedulerWithInterval(nil, time.Millisecond)
	StartWebhookReplayScheduler(nil)
}
