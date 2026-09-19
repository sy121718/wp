package webhookservice

// webhook_replay.go — 投递的可重放对账入口（AGENTS.md「写操作的事务与回滚」的跨库分支）。
//
// # 为什么「入队」不能和「写投递日志」放在同一个事务里
//
// 投递链上有两处写：
//
//   1. PostgreSQL 的 webhook_deliveries（pending 行 = 这次投递的存在证明，也是唯一真源）；
//   2. asynq / Redis 的任务队列（让 worker 尽快取走它）。
//
// 它们**跨系统**，没有共同的事务边界。两种顺序都有洞：
//
//   · 先入队、后写库（或把入队塞进事务）：worker 可能在事务提交前取到任务，
//     回库读不到那一行 → DeliverDelivery 按「日志不存在 = 任务无意义」返回成功 →
//     这次投递被静默丢掉，而日志行随后才提交，看起来一切正常。
//   · 先写库、后入队：入队在提交后失败（Redis 抖动 / 队列未启用），
//     那一行停在 pending 且**永远**不会被取走。
//
// 本模块选后者，并用「pending 行 = outbox」把第二个洞补上：
//
//   · 真源是**未落定的行**（pending 或 delivering），不是队列里的任务 —— 队列只是加速器，
//     丢了不影响正确性；
//   · ReplayPendingDeliveries 把两类「早就该有结果的」行重新入队：
//     ① pending 且超过正常入队延迟（入队丢了 / 队列从未启用）；
//     ② delivering 且认领租约已过期（worker 崩溃 / 卡死）。
//     **幂等**（worker 的认领守卫是「只有 pending、或租约已过期的 delivering 才投」，
//     重复入队最坏是多投一次 —— 这与队列自身重试的语义一致）+ **留痕**（逐条结构化日志，
//     抢占类单独计数）+ **可重放**（反复调用直到两类都清零，不会造成状态叠加）。
//
// 为什么不做「跨系统补偿事务」：补偿只在能精确判定「对方一定没做」时才有意义，
// 而队列无法查询「这条任务在不在」。所以这里不做猜测式的回滚，只做可重放的重投。

import (
	"context"
	"time"

	webhookenums "go_wp/internal/module/webhook/enums"
	"go_wp/pkg/logger"
)

// 重放参数默认值与上限。
const (
	replayDefaultLimit = 200
	replayMaxLimit     = 2000
	// replayMinAge 只重投「pending 已超过这个时长」的投递。
	//
	// 必须显著大于队列的正常投递延迟：刚派发完的 pending 很可能正在被 worker 处理，
	// 把它再入队一次就制造了一次重复投递。5 分钟对 webhook（秒级目标、15s 客户端超时）
	// 是足够宽松的余量 —— 真卡了 5 分钟，重投的收益远大于重复投一次的代价。
	replayMinAge = 5 * time.Minute
)

// ReplayPendingDeliveriesReq 重放参数。
type ReplayPendingDeliveriesReq struct {
	// Limit 本次最多重放多少条（默认 200，上限 2000）。
	Limit int
	// MinAge 覆盖默认的「pending 陈旧阈值」；<=0 用 replayMinAge。
	MinAge time.Duration
	// DeliverLease 覆盖默认的「delivering 认领租约」；<=0 用 deliverLease。
	//
	// 单独一个字段而不是复用 MinAge：两者判的不是同一件事（一个是「队列没把它捡起来」，
	// 一个是「捡起来的 worker 死了」）。合成一个可调参数，测试里就再也分不开这两种场景。
	DeliverLease time.Duration
}

// ReplayItem 单条重放记录（留痕）。
type ReplayItem struct {
	DeliveryID uint64 `json:"deliveryId"`
	EndpointID uint64 `json:"endpointId"`
	EventType  string `json:"eventType"`
	Attempts   int    `json:"attempts"`
	CreateTime string `json:"createTime"`
	// Reclaimed 这一条是从「租约过期的 delivering」抢回来的（worker 崩溃 / 卡死）。
	// 与「pending 重投」分开标：前者意味着**可能有一次已经发出去的请求**，
	// 排障时必须能一眼看出哪些行属于这种情形。
	Reclaimed bool   `json:"reclaimed"`
	Result    string `json:"result"`
}

// ReplayReport 重放报告。
//
// pending 与 delivering 分开计数：两种积压的**病因完全不同**（队列没启 / 端点没入队 vs
// worker 崩溃或卡死），压成一个数字会让排障的人看不出该去查哪里。
type ReplayReport struct {
	// PendingTotal 当前 pending 总数（含尚未达到陈旧阈值的，规模参考）。
	PendingTotal int64 `json:"pendingTotal"`
	// DeliveringTotal 当前 delivering 总数（正在投 / 认证领后卡住，规模参考）。
	DeliveringTotal int64 `json:"deliveringTotal"`
	Scanned         int   `json:"scanned"`
	Requeued        int   `json:"requeued"`
	// Reclaimed 其中来自「认领租约已过期」的条数（>0 意味着怀疑有 worker 死过，
	// 也意味着同一份事件可能被外部系统收到两次）。
	Reclaimed    int          `json:"reclaimed"`
	Failed       int          `json:"failed"`
	MinAge       string       `json:"minAge"`
	DeliverLease string       `json:"deliverLease"`
	Truncated    bool         `json:"truncated"`
	Items        []ReplayItem `json:"items"`
}

// ReplayPendingDeliveries 重放「早就该有结果」的投递：把长期停在 pending、或认领租约已过期的
// delivering 行重新入队。
//
// 幂等：重放**不改变任何投递行的状态**（只入队），worker 的认领守卫保证「已落定为
// delivered/failed 的行不会再投、租约未过期的 delivering 不会被别人抢」；重复调用只会对
// 同一批行再入队一次，最坏是多一次投递，不会叠加状态。
// 可重放：返回后两类仍有剩余（例如队列仍未恢复）时，再调一次即可继续。
//
// 抢占（把 delivering 抢回来）是**有代价**的：那条行的原认领者可能仍在网络上，请求可能已经
// 发出去了。所以它既在报告里单独计数、也在报告条目上打 Reclaimed，并单独记 Warn ——
// 让人知道「这一批里有多少条可能造成过重复投递」，而不是把它混进「pending 重投」这个
// 无害的数字里。
func (s *Service) ReplayPendingDeliveries(ctx context.Context, req *ReplayPendingDeliveriesReq) (*ReplayReport, error) {
	if req == nil {
		req = &ReplayPendingDeliveriesReq{}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = replayDefaultLimit
	}
	if limit > replayMaxLimit {
		limit = replayMaxLimit
	}
	minAge := req.MinAge
	if minAge <= 0 {
		minAge = replayMinAge
	}
	lease := req.DeliverLease
	if lease <= 0 {
		lease = deliverLease
	}

	report := &ReplayReport{MinAge: minAge.String(), DeliverLease: lease.String(), Items: []ReplayItem{}}
	pendingTotal, err := s.m.CountDeliveriesByStatus(ctx, webhookenums.DeliveryStatusPending)
	if err != nil {
		return nil, err
	}
	report.PendingTotal = pendingTotal
	deliveringTotal, derr := s.m.CountDeliveriesByStatus(ctx, webhookenums.DeliveryStatusDelivering)
	if derr != nil {
		return nil, derr
	}
	report.DeliveringTotal = deliveringTotal
	if pendingTotal == 0 && deliveringTotal == 0 {
		return report, nil
	}

	// 两个阈值都相对同一个 now 取：分成两次 time.Now() 会让「待重投」的集合在两次取材之间漂移。
	now := time.Now()
	rows, lerr := s.m.ListStaleDeliveries(ctx, now.Add(-minAge), now.Add(-lease), limit)
	if lerr != nil {
		return nil, lerr
	}
	report.Scanned = len(rows)
	report.Truncated = pendingTotal+deliveringTotal > int64(limit)
	for _, d := range rows {
		if ctx.Err() != nil {
			break
		}
		reclaimed := d.Status == webhookenums.DeliveryStatusDelivering
		if eerr := enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: d.ID}); eerr != nil {
			// 队列不可用：**不动任何状态**（那一行仍是原状态，下次重放会再试一次），
			// 记录原因后继续处理后面的行 —— 一条失败不该让整批停下。
			report.Failed++
			logger.Scene("webhook").With("delivery_id", d.ID).With("status", d.Status).
				With("queue_error", eerr.Error()).
				Error(eerr, "webhook 投递重放入队失败（该行状态未变，可再次重放）")
			continue
		}
		report.Requeued++
		if reclaimed {
			report.Reclaimed++
		}
		report.Items = append(report.Items, ReplayItem{
			DeliveryID: d.ID, EndpointID: d.EndpointID, EventType: d.EventType,
			Attempts:   d.Attempts,
			CreateTime: d.CreatedAt.Format(time.RFC3339),
			Reclaimed:  reclaimed,
			Result:     "已重新入队（worker 仅认领 pending 或租约过期的 delivering，重复入队幂等）",
		})
		if reclaimed {
			// 抢占类单独一条 Warn：原认领者可能已经把那次请求发出去了，外部系统可能收到两份。
			// 这是**要人知道**的事，不能和常规重投混在一行 Info 里。
			logger.Scene("webhook").With("delivery_id", d.ID).With("endpoint_id", d.EndpointID).
				With("event_type", d.EventType).With("attempts", d.Attempts).With("lease", lease.String()).
				Warn("webhook 投递重放：delivering 租约已过期，按 worker 崩溃/卡死抢占（该次投递可能已发出，留意重复）")
			continue
		}
		logger.Scene("webhook").With("delivery_id", d.ID).With("endpoint_id", d.EndpointID).
			With("event_type", d.EventType).With("attempts", d.Attempts).
			Info("webhook 投递重放：陈旧 pending 已重新入队")
	}
	logger.Scene("webhook").With("pending_total", report.PendingTotal).
		With("delivering_total", report.DeliveringTotal).
		With("scanned", report.Scanned).With("requeued", report.Requeued).
		With("reclaimed", report.Reclaimed).
		With("failed", report.Failed).With("min_age", report.MinAge).
		With("deliver_lease", report.DeliverLease).
		Warn("webhook 投递重放完成（只入队、不改状态；失败项下次可再重放）")
	return report, nil
}
