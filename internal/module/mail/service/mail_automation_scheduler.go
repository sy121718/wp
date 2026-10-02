package mailservice

// mail_automation_scheduler.go — 延时调度兜底（issue #38 P3）。
//
// 等待节点的**主路径**是队列的延时任务（EnqueueAt）。但主路径可能失效：
//   · 队列重启时任务丢失（asynq 的延时任务在 Redis 里，Redis 掉数据就没了）；
//   · 队列当时未启用（任务被静默跳过）；
//   · worker 崩溃在入队与执行之间。
//
// 所以需要一个扫描器：把「已到点但仍挂着」的实例重新投递。它是**幂等安全**的 ——
// 重复投递最多让执行器多跑一次，而执行器靠节点日志幂等（同一步不会做两次）。
//
// 扫描本身只做「扫一轮」（EnqueueDueRuns / TickDueRuns），周期由本文件的
// StartMailAutomationScheduler 提供；运维也仍可手工触发一轮（排障时很有用）。

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// mailAutomationTickInterval 兜底扫描的间隔。
	//
	// 为什么钉在 1 分钟：等待节点的最小粒度就是 1 分钟（图校验要求 minutes 是正整数，
	// 见 mail_automation_graph.go 的节点校验），扫描间隔取同一粒度意味着
	// 「到点」与「被重新投递」之间的最坏延迟不超过这张图能表达的最短等待时长。
	// 间隔再大就会让「等 1 分钟」的节点在最坏情况下多挂数倍时间，而扫描本身很轻
	//（status='waiting' AND next_run_at <= now，走既有索引，单轮最多 mailAutomationTickLimit 条）。
	mailAutomationTickInterval = time.Minute
	// mailAutomationTickLimit 单轮最多重新投递多少个实例（与 EnqueueDueRuns 的 limit 同义）。
	mailAutomationTickLimit = 500
	// mailAutomationTickTimeout 单轮扫描的超时：一轮卡住不该让 ticker 堆积。
	mailAutomationTickTimeout = 30 * time.Second
)

// EnqueueDueRuns 扫描已到点的等待实例并重新投递，返回投递数量。
//
// 队列未启用时 enqueueAutomationRun 是 no-op：此时投递数仍如实返回
// （它表示「本轮选中了多少个到点实例」），运营据此能看出「兜底在跑但队列是关的」。
func (s *Service) EnqueueDueRuns(ctx context.Context, limit int) (queued int, err error) {
	now := time.Now()
	runs, err := s.m.DueRuns(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	for _, run := range runs {
		enqueueAutomationRun(run.ID, time.Time{})
		queued++
	}
	return queued, nil
}

// TickDueRuns 扫一轮并返回扫到的实例数（由 StartMailAutomationScheduler 周期驱动，
// 也可由运维经后台「补投一轮」手工触发；语义同 EnqueueDueRuns）。
func (s *Service) TickDueRuns(ctx context.Context) (int, error) {
	return s.EnqueueDueRuns(ctx, mailAutomationTickLimit)
}

// StartMailAutomationScheduler 启动延时兜底的周期扫描（进程内 goroutine + ticker）。
//
// 与 StartMailRetentionScheduler 同形：测试进程不启动（首跑会动真实库）、
// 首跑一次再等 ticker、单轮 panic 收敛成日志（一轮的 bug 不该终止进程）。
//
// 为什么必须有人驱动：等待节点的唤醒**原本只有队列延时的主路径**，Redis 掉数据 /
// queue.enabled=false / worker 崩在入队与执行之间时，到点的实例会永远挂着 ——
// 而这条扫描正是 run 文件里写明的兜底保证。
func StartMailAutomationScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startMailAutomationScheduler(svc, mailAutomationTickInterval)
}

// StartMailAutomationSchedulerWithInterval 同上，但可注入间隔（测试用：
// 远大于用例时长的间隔可证成「首跑确实发生在启动时」，毫秒级间隔可证成「等间隔在驱动」）。
func StartMailAutomationSchedulerWithInterval(svc *Service, interval time.Duration) {
	startMailAutomationScheduler(svc, interval)
}

func startMailAutomationScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runMailAutomationTickLoop(func() {
		// goroutine 里未 recover 的 panic 会终止整个进程：一轮的 bug 只该让这一轮没有结论。
		defer func() {
			if r := recover(); r != nil {
				logger.Scene("mail").Error(fmt.Errorf("panic: %v", r),
					"自动化延时兜底扫描单轮 panic（已收敛，下一轮照常；不影响进程）")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), mailAutomationTickTimeout)
		defer cancel()
		queued, err := svc.TickDueRuns(ctx)
		if err != nil {
			logger.Scene("mail").Error(err, "自动化延时兜底扫描失败（下一轮重试）")
			return
		}
		if queued > 0 {
			logger.Scene("mail").With("queued", queued).
				Info("自动化延时兜底：已重新投递到点的等待实例")
		}
	}, interval)
}

// runMailAutomationTickLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测（与 media 巡检的 runMediaReconcileLoop 同形）：
// 目标动作要数据库与队列，模块内单测不碰这些依赖，但**调度语义**（首跑 / 等间隔 /
// 单轮 panic 不致命）与动作内容无关 —— 这三件事写反了的表现都是「看起来在跑、其实没动」。
//
// 返回的 stop 关闭后循环退出；生产装配不调用它（进程退出即结束），测试用它收尾。
func runMailAutomationTickLoop(run func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = mailAutomationTickInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		run() // 首跑：与 retention / media 巡检同一节奏（不是先等一个间隔）
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				run()
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}
