package webhookservice

// webhook_dispatch.go — 事件发布入口：按事件类型查已启用端点，逐个建投递记录并入队。
// 只入队、不出站：慢目标或超时不会拖住发布方的事务。

import (
	"context"
	"fmt"
	"time"

	"encoding/json"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"gorm.io/gorm"
)

// ---- 事件分发 ----

// DispatchEvent 向某事件类型的全部启用端点派发一次投递：
// 每个端点建一条 pending 投递日志并入队，由 worker 异步签名发送。
// 返回成功入队的端点数。事件负载超限整体拒绝（不静默截断）。
func (s *Service) DispatchEvent(ctx context.Context, eventType string, payload any) (n int, err error) {
	body, merr := json.Marshal(payload)
	if merr != nil {
		return 0, fmt.Errorf("webhook 事件负载序列化失败: %w", merr)
	}
	if len(body) > MaxPayloadBytes {
		return 0, fmt.Errorf("webhook 事件负载 %d 字节超出上限 %d", len(body), MaxPayloadBytes)
	}

	endpoints, lerr := s.m.ListEndpoints(ctx, eventType, true)
	if lerr != nil {
		return 0, lerr
	}
	now := time.Now()
	// 扇出的 N 条投递日志**同一个事务**（AGENTS.md「写操作的事务与回滚」）：
	// 逐条各自提交时中途失败会留下「一部分端点有日志、一部分没有」—— 少了日志的那些
	// 端点**永远不会**收到这次事件（没有任何东西会再来派发它），而调用方看到的错误
	// 只是「本次派发失败」，重试又会给已经拿到日志的端点再发一遍。
	rows := make([]*webhookmodel.WebhookDeliveryEntity, 0, len(endpoints))
	for _, ep := range endpoints {
		rows = append(rows, &webhookmodel.WebhookDeliveryEntity{
			EndpointID: ep.ID,
			EventType:  eventType,
			Payload:    string(body),
			Status:     webhookenums.DeliveryStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if terr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.CreateDeliveriesTx(ctx, tx, rows)
	}); terr != nil {
		return 0, terr
	}

	// 入队在事务**之外**：队列（asynq/Redis）是另一个系统，进不了 PG 事务。
	// 队列侧的「入队」与 DB 侧的「投递日志」不是真正的两处持久化写入，而是
	// 「真源 + 派生动作」：日志是唯一真源（pending 即待投），入队只是让它更快被取走。
	// 所以这里不做跨系统补偿事务，而是靠可重放的对账入口补齐（webhook_replay.go）。
	//
	// 为什么不能反过来把入队塞进事务：worker 可能在事务提交前就取到任务，
	// 回库读不到那一行 → DeliverDelivery 按「日志不存在 = 任务无意义」返回成功 →
	// 这次投递被静默丢掉，且日志行随后才提交，看起来完全正常。
	for _, d := range rows {
		if eerr := enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: d.ID}); eerr != nil {
			// 队列不可用：日志保留 pending，返回错误让调用方感知，不假装已派发。
			// 已入队的部分不回收（回收会让它们再也不被投递）；未入队的那部分由
			// ReplayPendingDeliveries 重放（幂等：worker 只认领 pending 或租约过期的 delivering）。
			return n, eerr
		}
		n++
	}
	return n, nil
}
