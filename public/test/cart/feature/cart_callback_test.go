package feature

// cart_callback_test.go — 支付通道异步回调的链路测试（BIZ-1）。
//
// 覆盖的是**不变量**而不是「方法能跑通」：
//   · 验签失败一律拒绝，且不碰订单一列（回调是未认证输入，签名是唯一来源证明）；
//   · 金额核对：与订单总额不符时停在人工核对，不入账；
//   · 幂等：通道重发通知是常态，重复必须无害；
//   · 失败通知不改状态（订单本就停在待付款），但要有明确结论。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	mockpaypal "go_wp/internal/module/cart/outbound/mockpaypal"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

// payCallback 造一条回调请求（签名用给定密钥算，便于测篡改）。
func (f *cartFixture) payCallback(t *testing.T, orderNo string, amount int64, status, secret string) (*cartdto.PaymentCallbackResp, error) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"orderNo": orderNo, "transactionId": "TX-FROM-CHANNEL",
		"status": status, "amount": amount, "currency": "CNY",
	})
	if err != nil {
		t.Fatalf("序列化回调报文失败: %v", err)
	}
	return f.cart.HandlePaymentCallback(context.Background(), &cartdto.PaymentCallbackReq{
		ProjectID: f.projectID,
		Headers:   map[string]string{"x-mock-signature": mockpaypal.Sign(secret, body)},
		RawBody:   body,
	})
}

// pendingOrder 造一张待付款订单，返回订单号与金额（分）。
func (f *cartFixture) pendingOrder(t *testing.T, variantID string, qty int) (orderNo string, total int64) {
	t.Helper()
	res, err := f.orders.CreateOrder(context.Background(), &orderdto.CreateOrderReq{
		ProjectID:     f.projectID,
		CustomerEmail: "callback@example.com",
		CustomerName:  "回调买家",
		Items:         []orderdto.OrderItemReq{{VariantID: variantID, Quantity: qty}},
		Shipping:      orderdto.OrderAddress{Name: "回调买家", Phone: "13800000000", City: "深圳", Address: "某某路 1 号"},
	})
	if err != nil {
		t.Fatalf("建待付款订单失败: %v", err)
	}
	return res.OrderNo, res.Total
}

// TestPaymentCallbackPaysOrder 合法回调 → 订单入账 + 流水号落地。
func TestPaymentCallbackPaysOrder(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "回调商品", 50, 5)
	orderNo, total := f.pendingOrder(t, vid, 2)

	res, err := f.payCallback(t, orderNo, total, "success", cartTestSecret)
	if err != nil {
		t.Fatalf("合法回调应入账: %v", err)
	}
	if !res.Paid || !res.Applied || res.Already {
		t.Fatalf("结论不对: %+v", res)
	}
	if res.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("订单应推进到 paid，实际 %s", res.Status)
	}

	detail, err := f.orders.GetOrder(context.Background(), res.OrderID)
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.TransactionID != "TX-FROM-CHANNEL" {
		t.Fatalf("通道流水号必须落在订单上（对账要看它），实际 %q", detail.Head.TransactionID)
	}
	if detail.Head.PaidAt == nil {
		t.Fatal("入账必须写支付时间")
	}
}

// TestPaymentCallbackIsIdempotent 通道重发通知：第二次不改任何列，如实回报「此前已付」。
func TestPaymentCallbackIsIdempotent(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "幂等商品", 20, 5)
	orderNo, total := f.pendingOrder(t, vid, 1)

	if _, err := f.payCallback(t, orderNo, total, "success", cartTestSecret); err != nil {
		t.Fatalf("首次回调失败: %v", err)
	}
	second, err := f.payCallback(t, orderNo, total, "success", cartTestSecret)
	if err != nil {
		t.Fatalf("重复回调不该报错（通道会一直重发）: %v", err)
	}
	if !second.Already || second.Applied {
		t.Fatalf("重复回调应命中幂等且未改动任何列，实际 %+v", second)
	}
}

// TestPaymentCallbackRejectsTamperedSignature 签名不对 → 拒绝，且订单一列不动。
func TestPaymentCallbackRejectsTamperedSignature(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "验签商品", 30, 5)
	orderNo, total := f.pendingOrder(t, vid, 1)
	ctx := context.Background()

	// 用错误密钥签名（攻击者不知道真密钥）。
	_, err := f.payCallback(t, orderNo, total, "success", "attacker-secret")
	if err == nil || !strings.Contains(err.Error(), cartenums.ErrCallbackSignature) {
		t.Fatalf("错误签名必须被拒绝，实际: %v", err)
	}
	// 完全没有签名头。
	body, _ := json.Marshal(map[string]any{"orderNo": orderNo, "status": "success", "amount": total})
	if _, err := f.cart.HandlePaymentCallback(ctx, &cartdto.PaymentCallbackReq{
		ProjectID: f.projectID, Headers: map[string]string{}, RawBody: body,
	}); err == nil || !strings.Contains(err.Error(), cartenums.ErrCallbackSignature) {
		t.Fatalf("缺少签名必须被拒绝，实际: %v", err)
	}

	// 订单必须还停在待付款。
	detail, _ := f.orders.GetOrderByNo(ctx, &orderdto.GetOrderByNoReq{ProjectID: f.projectID, OrderNo: orderNo})
	if detail == nil || detail.Status != ordermodel.OrderStatusPending {
		t.Fatalf("验签失败不该改动订单，实际: %+v", detail)
	}
}

// TestPaymentCallbackRejectsAmountMismatch 金额不符 → 拒绝入账（账目不平宁可停下来看）。
func TestPaymentCallbackRejectsAmountMismatch(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "金额商品", 60, 5)
	orderNo, total := f.pendingOrder(t, vid, 1)
	ctx := context.Background()

	_, err := f.payCallback(t, orderNo, total-100, "success", cartTestSecret)
	if err == nil || !strings.Contains(err.Error(), cartenums.ErrCallbackAmountMismatch) {
		t.Fatalf("金额不符必须被拒绝，实际: %v", err)
	}
	detail, _ := f.orders.GetOrderByNo(ctx, &orderdto.GetOrderByNoReq{ProjectID: f.projectID, OrderNo: orderNo})
	if detail == nil || detail.Status != ordermodel.OrderStatusPending {
		t.Fatalf("金额不符不该入账，实际: %+v", detail)
	}

	// 通道没报金额（Amount=0）表示「未提供」而不是「零元」：跳过核对而不是判成不符。
	res, err := f.payCallback(t, orderNo, 0, "success", cartTestSecret)
	if err != nil || !res.Applied {
		t.Fatalf("通道未报金额时应跳过核对并入账，实际: %v / %+v", err, res)
	}
}

// TestPaymentCallbackFailedNotificationKeepsPending 失败通知：不改状态，但给出明确结论。
func TestPaymentCallbackFailedNotificationKeepsPending(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "失败通知商品", 70, 5)
	orderNo, total := f.pendingOrder(t, vid, 1)
	ctx := context.Background()

	res, err := f.payCallback(t, orderNo, total, "failed", cartTestSecret)
	if err != nil {
		t.Fatalf("失败通知是正常业务消息，不该报错: %v", err)
	}
	if res.Paid || res.Applied {
		t.Fatalf("失败通知不该改动订单，实际: %+v", res)
	}
	if strings.TrimSpace(res.Message) == "" {
		t.Fatal("失败通知也要给出可读结论（否则日志里只剩一行空白）")
	}
	detail, _ := f.orders.GetOrderByNo(ctx, &orderdto.GetOrderByNoReq{ProjectID: f.projectID, OrderNo: orderNo})
	if detail == nil || detail.Status != ordermodel.OrderStatusPending {
		t.Fatalf("失败通知后订单应保持待付款，实际: %+v", detail)
	}
}

// TestPaymentCallbackUnknownOrderRejected 单号找不到：明确拒绝，不静默吞掉。
func TestPaymentCallbackUnknownOrderRejected(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, err := f.payCallback(t, "NOT-A-REAL-ORDER", 100, "success", cartTestSecret)
	if err == nil || !strings.Contains(err.Error(), cartenums.ErrCallbackOrderMissing) {
		t.Fatalf("未知单号必须被拒绝，实际: %v", err)
	}
}

// TestPaymentCallbackRejectsWhenGatewayHasNoSecret 通道没配密钥时拒绝一切回调。
//
// 这一条守的是「降级方向」：没密钥就意味着无法判断来源，此时**信任所有回调**
// 是灾难性的默认值（任何人都能把订单改成已付款）。
func TestPaymentCallbackRejectsWhenGatewayHasNoSecret(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(""))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "无密钥商品", 25, 5)
	orderNo, total := f.pendingOrder(t, vid, 1)
	_, err := f.payCallback(t, orderNo, total, "success", "")
	if err == nil {
		t.Fatal("未配置回调密钥时必须拒绝回调，而不是信任它")
	}
}
