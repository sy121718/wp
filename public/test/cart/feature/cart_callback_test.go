package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	mockpaypal "go_wp/internal/module/cart/outbound/mockpaypal"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

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

	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
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

func TestPaymentCallbackRejectsTamperedSignature(t *testing.T) {
	f := newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "验签商品", 30, 5)
	orderNo, total := f.pendingOrder(t, vid, 1)
	ctx := context.Background()

	_, err := f.payCallback(t, orderNo, total, "success", "attacker-secret")
	if err == nil || !strings.Contains(err.Error(), cartenums.ErrCallbackSignature) {
		t.Fatalf("错误签名必须被拒绝，实际: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"orderNo": orderNo, "status": "success", "amount": total})
	if _, err := f.cart.HandlePaymentCallback(ctx, &cartdto.PaymentCallbackReq{
		ProjectID: f.projectID, Headers: map[string]string{}, RawBody: body,
	}); err == nil || !strings.Contains(err.Error(), cartenums.ErrCallbackSignature) {
		t.Fatalf("缺少签名必须被拒绝，实际: %v", err)
	}

	detail, _ := f.orders.GetOrderByNo(ctx, &orderdto.GetOrderByNoReq{ProjectID: f.projectID, OrderNo: orderNo})
	if detail == nil || detail.Status != ordermodel.OrderStatusPending {
		t.Fatalf("验签失败不该改动订单，实际: %+v", detail)
	}
}

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

	res, err := f.payCallback(t, orderNo, 0, "success", cartTestSecret)
	if err != nil || !res.Applied {
		t.Fatalf("通道未报金额时应跳过核对并入账，实际: %v / %+v", err, res)
	}
}

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
