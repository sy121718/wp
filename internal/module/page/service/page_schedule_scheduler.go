package pageservice

// page_schedule_scheduler.go — 定时上下线的到点扫描调度器（PIPE-7）。
//
// 形状与 page_retention.go / page_publish_converge.go 一致：进程内 goroutine + ticker，
// 先跑一次再等间隔。启动点在同模块的路由装配处（page_router.go），与那两个调度器并排 ——
// 模块自己的调度器在模块内启动，不经由任何外部注册表。
//
// 为什么不是 asynq / 外部队列：queue.enabled 与 run_worker 的默认值是 false
// （config.yaml.example），把「到点上线」挂在队列上会让**未启用队列的部署静默不生效** ——
// 排定照常写进数据库、到点什么都不发生，而界面上一切正常。进程内 ticker 是唯一
// 不依赖部署开关的形态；队列将来只能作可选加速层，且必须有 ticker 兜底。
//
// 多实例安全性：重复扫描是安全的（认领是一条带 FOR UPDATE SKIP LOCKED 的原子语句，
// 同一瞬间只有一个实例领到同一条排定）；重复执行也是安全的（切指针幂等、
// 数据库步骤幂等、DS 步骤都能重放）。因此这里**不做**跨实例的选主。

import (
	"context"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// StartPageScheduleScheduler 启动定时上下线的到点扫描。
func StartPageScheduleScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发。
	}
	startPageScheduleScheduler(svc, pageScheduleInterval)
}

// StartPageScheduleSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入一个远大于用例时长的间隔，就能证明「这次状态变化确实由用例自己触发的
// 那一轮扫描产生」，而不是被定时器顺手做掉的（与 pendingReceiptConverge 的
// Start...WithInterval 同一用意）。
func StartPageScheduleSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPageScheduleScheduler(svc, interval)
}

func startPageScheduleScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pageScheduleInterval
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pageScheduleScanTimeout)
			defer cancel()
			if _, err := svc.RunDueSchedules(ctx); err != nil {
				logger.Scene("page").Error(err, "定时上下线扫描失败")
			}
		}
		// 启动首跑：补上进程停机期间到点的排定 —— 「到点」是绝对时刻，
		// 不因为进程当时没在运行而顺延（排定界面显示的也是那个时刻）。
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
