package webhookservice

// webhook_replay_scheduler.go —— 「陈旧 pending 投递重放」的进程内调度。
//
// 为什么必须有这个文件：webhook_replay.go 的 ReplayPendingDeliveries 此前**没有任何
// 调用方**，而它存在的理由恰恰是「入队在提交后失败（Redis 抖动 / 队列未启用）时那一行
// 会永远停在 pending」（见该文件头部对两种写入顺序与各自的洞的分析）。没有时间驱动，
// 那个「永远」就是字面意思 —— 只有人恰好发现才会重投。
//
// 形状与既有调度器逐项一致（page_publish_converge.go / analytics_retention.go /
// mail_retention.go / order_expire.go）：首跑一次再按 ticker 等间隔、ctx 用
// context.Background()、目标方法报错只记日志、任何 panic 一律 recover 不拖垮进程。
//
// 纳入判断（与 media 的变体补偿同一把尺子）：ReplayPendingDeliveries 的文档注释明写
// **幂等**（重放只入队、不改任何投递行状态；worker 的守卫是「只有 pending 才投」，
// 重复入队最坏是多投一次）+ **可重放**（pending 未清零时再调一次即可继续），
// 且它重放的是「尚未落定的投递任务」这一**派生的工作项**，不是篡改已定案的数据 ——
// 满足「幂等 + 只动可重新生成的东西」，因此纳入调度。
//
// 本文件不复制任何判据：陈旧阈值仍是 webhook_replay.go 的 replayMinAge，
// 这里只决定「多久驱动一次」。

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// webhookReplayInterval 重放间隔。
	//
	// 取值必须**显著大于** replayMinAge（webhook_replay.go，5 分钟）：MinAge 是「陈旧」
	// 的判据 —— 比它新的 pending 很可能正在被 worker 处理，把它再入队一次就是制造一次
	// 重复投递。1 小时 = MinAge 的 12 倍：既给 worker 足够余量（正常投递是秒级、
	// 客户端超时 15s），又让「入队丢了」的投递在一个小时内被发现并重投 ——
	// 而这个窗口的缩短正是本调度的全部价值。
	//
	// 反过来，间隔若接近或小于 MinAge，每一轮都会对着同一批刚派发的 pending 重投，
	// 退化成「把重复投递做成了常态」。
	webhookReplayInterval = time.Hour
	// webhookReplayTimeout 单轮重放的超时。
	//
	// 一轮的重活全在入队（Redis 往返）上：即便队列整个不可用，失败也是快速返回，
	// 5 分钟足够处理上限 2000 条。
	webhookReplayTimeout = 5 * time.Minute
)

// webhookReplayRoundResult 一轮重放的结论（真实计数，供日志与用例断言）。
type webhookReplayRoundResult struct {
	err          error
	pendingTotal int64
	scanned      int
	requeued     int
	failed       int
	minAge       string
	truncated    bool
}

// runWebhookReplayRound 跑一轮重放：调既有的 ReplayPendingDeliveries，把 panic 收敛成
// 本轮失败。
//
// 参数用 nil 让 service 走它自己的默认值（limit / minAge）—— 调度器不该在这里
// 抄一份默认值，那是第二份真相。
func runWebhookReplayRound(ctx context.Context, svc *Service) (res webhookReplayRoundResult) {
	if svc == nil {
		return res
	}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("panic: %v", r)
		}
	}()
	report, err := svc.ReplayPendingDeliveries(ctx, nil)
	if err != nil {
		res.err = err
		return res
	}
	res.pendingTotal = report.PendingTotal
	res.scanned = report.Scanned
	res.requeued = report.Requeued
	res.failed = report.Failed
	res.minAge = report.MinAge
	res.truncated = report.Truncated
	return res
}

// runWebhookReplayLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测：目标方法要数据库与队列，模块内单测不碰这些
// 依赖，但调度语义（首跑 / 等间隔 / 单轮 panic 不致命）与动作内容无关。
// 返回的 stop 关闭后循环退出（生产不调用，测试用它收尾）。
func runWebhookReplayLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = webhookReplayInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		// 首跑：与样板一致，先跑一次再等 ticker（不是先等一个间隔）。
		safeWebhookReplayRound(round)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				safeWebhookReplayRound(round)
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// safeWebhookReplayRound 执行一轮并把 panic 收敛成日志。
//
// goroutine 里未被 recover 的 panic 会直接终止整个进程 —— 重放任务没有这种权力：
// 一轮动作的 bug 只该让这一轮没有结论，下一个间隔照常再来。
func safeWebhookReplayRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("webhook").Error(fmt.Errorf("panic: %v", r),
				"webhook 投递重放单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

// StartWebhookReplayScheduler 启动陈旧 pending 投递的重放调度（进程内 goroutine + ticker）。
func StartWebhookReplayScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startWebhookReplayScheduler(svc, webhookReplayInterval)
}

// StartWebhookReplaySchedulerWithInterval 同上，但可注入间隔（用例用）。
func StartWebhookReplaySchedulerWithInterval(svc *Service, interval time.Duration) {
	startWebhookReplayScheduler(svc, interval)
}

func startWebhookReplayScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runWebhookReplayLoop(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), webhookReplayTimeout)
		defer cancel()
		res := runWebhookReplayRound(ctx, svc)
		logWebhookReplayRound(res, time.Since(start))
	}, interval)
}

// logWebhookReplayRound 每轮记**一条**结构化日志（真实计数 + 耗时）。
//
// 与 service 内部日志的分工：ReplayPendingDeliveries 内部已有一条「本次重放做了什么」
// 的汇总 Warn，这一条回答「这一轮调度跑完了没、耗时多少」。两者读同一份报告结构体，
// 没有第二份判据。
//
// 分档：动作失败 → Error；有仍失败的 pending 或重放了任何一条 → Warn（重放条数 > 0
// 本身就意味着「有投递长期停在 pending」，那是需要人知道的不一致；失败条数 > 0 更是）；
// 否则 Info。
func logWebhookReplayRound(res webhookReplayRoundResult, cost time.Duration) {
	entry := logger.Scene("webhook").
		With("pending_total", res.pendingTotal).
		With("scanned", res.scanned).
		With("requeued", res.requeued).
		With("failed", res.failed).
		With("min_age", res.minAge).
		With("truncated", res.truncated).
		With("cost_ms", cost.Milliseconds())
	switch {
	case res.err != nil:
		entry.Error(res.err, "webhook 投递重放调度：本轮动作失败（下一轮重试；pending 行不受影响）")
	case res.failed > 0:
		entry.Warn("webhook 投递重放完成：部分投递入队失败，仍留在 pending 等待下一轮")
	case res.requeued > 0:
		entry.Warn("webhook 投递重放完成：有投递长期停在 pending，已重新入队（队列此前丢过任务，需人工看目标是否可达）")
	default:
		entry.Info("webhook 投递重放完成：没有陈旧 pending（队列正常）")
	}
}
