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
// 扫描不自己做定时：由调用方（周期任务 / 运维命令 / 路由）驱动，本函数只负责「扫一轮」。
// 这样它既能被队列的周期任务用，也能被手工触发（排障时很有用）。

import (
	"context"
	"time"
)

// EnqueueDueRuns 扫描已到点的等待实例并重新投递，返回投递数量。
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

// TickDueRuns 扫一轮并返回扫到的实例数（供周期任务调用，语义同 EnqueueDueRuns）。
func (s *Service) TickDueRuns(ctx context.Context) (int, error) {
	return s.EnqueueDueRuns(ctx, 500)
}
