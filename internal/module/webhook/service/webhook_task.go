package webhookservice

// webhook_task.go — webhook 投递的队列任务接入（复用 pkg/queue，与 mail 同形状）。
//
// 投递一律异步：事件发布方（订单、内容等模块）只调 DispatchEvent，
// 真正的出站 HTTP 由 worker 执行 —— 慢目标或超时不会拖住业务请求。

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/queue"
)

// TaskWebhookDeliver 投递任务类型。
const TaskWebhookDeliver = "webhook:deliver"

// WebhookDeliverPayload 投递载荷。
//
// 只带 DeliveryID：worker 回库取端点与负载 —— 端点密钥与 URL 以投递时刻
// 数据库为准（管理员改 URL / 停用端点对未派发任务立即生效）。
type WebhookDeliverPayload struct {
	DeliveryID uint64 `json:"delivery_id"`
}

// webhookDeliverTask 队列任务门面（MaxRetry 即投递重试上限）。
var webhookDeliverTask = queue.NewTask(TaskWebhookDeliver, queue.WithQueue("default"), queue.WithMaxRetry(3))

// enqueueWebhookDeliver 入队一次投递。
func enqueueWebhookDeliver(p WebhookDeliverPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步投递 webhook")
	}
	return webhookDeliverTask.Enqueue(p)
}

// RegisterWebhookTaskHandler 把 handler 注册进队列 worker（路由装配时调用）。
func RegisterWebhookTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskWebhookDeliver, handleWebhookDeliver(db, cipherSecret))
}

// handleWebhookDeliver 投递 handler。
func handleWebhookDeliver(db *gorm.DB, cipherSecret string) queue.Handler {
	return func(ctx context.Context, raw []byte) error {
		var p WebhookDeliverPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.DeliveryID == 0 {
			return errors.New("投递任务载荷缺少 delivery_id")
		}
		svc := NewService(webhookmodel.NewWebhookModel(db))
		svc.SetCipherSecret(cipherSecret)
		return svc.DeliverDelivery(ctx, p.DeliveryID)
	}
}
