package orderservice

// order_events.go — 订单域对外事件的派发（OSS-006 外部集成通道）。
//
// 这里是「业务事实 → 外部通知」的唯一转换点。三条设计约束：
//
//  1. 通知**不是事务的一部分**：一律在业务事务提交之后派发，失败只记日志 ——
//     对端收不到通知不该让已经成立的事实回滚（钱已经收了，不能因为 webhook 挂了退回去）。
//     跨模块事务不存在，也不该为一次通知造一个。
//  2. 载荷只放**不可变事实**：订单号、金额、币种、支付方式、交易号。
//     刻意不含客户邮箱 / 手机号 / 收货地址 —— 端点是我们自己登记的，但它拿到的东西
//     不该多于完成这件事所需（数据最小化；投递日志还会把载荷原文留在库里）。
//  3. 派发口是**可选依赖**：未接线时整体跳过。webhook 是插件生态的外部能力，
//     不是订单域的必需品，缺它不该让订单模块起不来。

import (
	"context"
	"time"

	"go_wp/pkg/logger"
)

// OrderPaidEvent 订单支付成功事件的载荷（webhook 事件名 order.paid）。
//
// 金额一律是**最小币种单位的整数**（与 orders.total 同口径，人民币即「分」）：
// 浮点金额在跨语言的 JSON 通道上会出现 0.1+0.2 这类误差，接收方拿到 19.99
// 还是 19.989999 取决于它用哪个解析器 —— 整数分是精确的，换算责任交给展示层。
type OrderPaidEvent struct {
	// OrderID 订单主键（内部流水 id，接收方一般用 OrderNo 做业务引用）。
	OrderID uint64 `json:"orderId"`
	// OrderNo 订单号（对外业务引用）。
	OrderNo string `json:"orderNo"`
	// Status 支付后的订单状态（恒为 paid）。
	Status string `json:"status"`
	// Currency 币种代码。
	Currency string `json:"currency"`
	// Total 应付总额（最小币种单位）。
	Total int64 `json:"total"`
	// Subtotal / DiscountTotal / ShippingTotal / TaxTotal 金额分解（同口径）。
	Subtotal      int64 `json:"subtotal"`
	DiscountTotal int64 `json:"discountTotal"`
	ShippingTotal int64 `json:"shippingTotal"`
	TaxTotal      int64 `json:"taxTotal"`
	// PaymentMethod 支付方式标识（网关代号或手工登记的方式）。
	PaymentMethod string `json:"paymentMethod"`
	// TransactionID 通道流水号（手工登记 / 模拟通道可能为空）。
	TransactionID string `json:"transactionId"`
	// PaidAt 支付落账时刻。
	PaidAt time.Time `json:"paidAt"`
}

// dispatchEvent best-effort 派发一次对外事件：未接线即跳过，失败只记日志。
//
// 返回的入队条数刻意不透出：调用方（支付落账）拿它做不了任何决定，
// 透出只会诱使上层写「派发给几个端点」这类依赖外部配置的分支。
func (s *Service) dispatchEvent(ctx context.Context, eventType string, payload any) {
	if s.webhooks == nil {
		return // 未接线：外部通道未开启，不是错误，也不是降级。
	}
	if _, err := s.webhooks.DispatchEvent(ctx, eventType, payload); err != nil {
		// 只记日志：通知失败不能影响已经落账的支付。
		logger.Error(err, "订单事件派发失败（不影响业务事实）："+eventType)
	}
}
