package webhookcontract

// webhook_service.go — webhook 模块对外契约。
//
// 只放本模块对外暴露的能力，不定义外部依赖接口。
// 签名密钥（secret_cipher）永不经过这里：端点管理面的响应只带 HasSecret 布尔，
// 明文密钥只在 service 内部进出加密层。

import (
	"context"

	webhookdto "go_wp/internal/module/webhook/dto"
)

// 事件类型常量。
//
// 放在 contract 而不是 enums：事件名是**跨模块协议** —— 发布方（订单、内容……）
// 与订阅方（管理员在后台登记端点时填的事件名）按它对齐。enums 只管本模块的响应文案。
//
// event_type 在库里是自由字符串（插件可以贴任意事件名），这里只固化**本系统已有
// 派发点**的事件名；新增派发点要同时在 EndpointService 的契约文档里补一行。
const (
	// EventOrderPaid 订单支付成功（order 模块 PayOrder 落账后派发）。
	EventOrderPaid = "order.paid"
)

// EndpointService webhook 端点管理与投递排障（后台配置面）。
type EndpointService interface {
	// CreateEndpoint 新建端点：URL 立即过 SSRF 校验，密钥加密落库。
	CreateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error)
	// UpdateEndpoint 更新端点（req.ID 必填）。改 URL / 密钥时分别重新校验与重新加密。
	UpdateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error)
	DeleteEndpoint(ctx context.Context, id uint64) (err error)
	// SetEndpointStatus 启停端点（停用后不再派发新投递；已入队的投递仍会执行）。
	SetEndpointStatus(ctx context.Context, id uint64, status int) (err error)
	// ListEndpoints 列出端点（eventType 为空即全部）。
	ListEndpoints(ctx context.Context, eventType string) (list []*webhookdto.EndpointItem, err error)
	// ListDeliveries 投递日志（排障视图）。
	ListDeliveries(ctx context.Context, req *webhookdto.DeliveryListReq) (res *webhookdto.DeliveryListResp, err error)
	// RetryDelivery 重投一次失败的投递（重置为 pending 并重新入队）。
	RetryDelivery(ctx context.Context, id uint64) (err error)
}

// Dispatcher 事件分发端口。
//
// 单独一个接口而不并进 EndpointService：事件发布方（订单、内容……）只需要
// 「向订阅者各排一次投递」这一条能力，端点 CRUD 与投递日志对它毫无用处 ——
// 拿到整个 EndpointService 只会扩大误用面。越权防护靠**接口形状**，不靠调用方自觉。
// 与 mail 模块的 TransactionalSender / TrackingService 同一手法。
type Dispatcher interface {
	// DispatchEvent 向订阅了 eventType 的全部启用端点各排一次投递，返回入队条数。
	//
	// 语义是 **best-effort 通知，不是事务的一部分**：
	//   · 调用方必须在自己的业务事务**提交之后**调用（跨模块事务不存在，也不该为通知造一个）；
	//   · 返回的 error 表示「通知没能排出去」，调用方只记日志 ——
	//     绝不能因为外部集成没接上而回滚已经成立的业务事实（付过的钱不会因为 webhook 挂了而退回）；
	//   · 返回 0 条且无 error 是正常情况：该事件没有任何启用端点订阅，说明没人关心。
	DispatchEvent(ctx context.Context, eventType string, payload any) (queued int, err error)
}
