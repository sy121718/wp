package feature

import (
	"context"
	"strings"
	"testing"

	cartdto "go_wp/internal/module/cart/dto"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

func TestCartFreeCheckoutMarksPaid(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "免费商品", 0, 10)

	snap := f.addToCart(t, variantID, 1, "")
	if snap.Total != 0 {
		t.Fatalf("前置条件不成立：0 元商品的小计应为 0，实际 %d", snap.Total)
	}

	res, err := f.cart.Checkout(ctx, f.checkoutReq(snap.Cookie))
	if err != nil {
		t.Fatalf("0 元订单结算失败: %v", err)
	}
	if !res.Paid {
		t.Fatalf("0 元订单应直接落账（Paid=true），实际 Paid=false（%s）", res.PaymentError)
	}
	if res.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("结算响应状态应为 paid，实际 %q", res.Status)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}

	if detail.Head.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("0 元订单落库状态应为 paid，实际 %q（停在 pending 即缺陷复现）", detail.Head.Status)
	}
	if detail.Head.PaidAt == nil {
		t.Fatal("已付款订单必须有 paid_at")
	}

	if detail.Head.PaymentMethod != "free" {
		t.Fatalf("0 元订单的落账通道应为 free，实际 %q", detail.Head.PaymentMethod)
	}
	if !strings.HasPrefix(detail.Head.TransactionID, "FREE-") {
		t.Fatalf("0 元订单流水号应带 FREE- 前缀（由订单号派生、可对账），实际 %q", detail.Head.TransactionID)
	}
	if !strings.Contains(detail.Head.TransactionID, detail.Head.OrderNo) {
		t.Fatalf("0 元流水号应由订单号派生（幂等），实际 %q / 单号 %q",
			detail.Head.TransactionID, detail.Head.OrderNo)
	}
	if detail.Head.Total != 0 {
		t.Fatalf("订单总额应为 0，实际 %d", detail.Head.Total)
	}
}

func TestCartFreeCheckoutIsIdempotentByRequestID(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "免费商品幂等", 0, 10)
	snap := f.addToCart(t, variantID, 1, "")

	req := f.checkoutReq(snap.Cookie)
	req.RequestID = "free-checkout-idem-1"
	first, err := f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("首次结算失败: %v", err)
	}
	if !first.Paid {
		t.Fatalf("首次结算应落账，实际 Paid=false（%s）", first.PaymentError)
	}

	second, err := f.cart.Checkout(ctx, req)
	if err != nil {
		t.Fatalf("重复结算失败: %v", err)
	}
	if second.OrderID != first.OrderID {
		t.Fatalf("同一 requestId 必须命中同一张单：%d vs %d", first.OrderID, second.OrderID)
	}
	if !second.Paid || second.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("重复结算应返回已付款结论（PayOrder 幂等），实际 Paid=%v status=%q",
			second.Paid, second.Status)
	}
}

func TestCartPaidOrderRequiresPositiveAmountGateway(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "收费商品", 12.5, 5)
	snap := f.addToCart(t, variantID, 2, "")

	res, err := f.cart.Checkout(ctx, f.checkoutReq(snap.Cookie))
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if !res.Paid {
		t.Fatalf("收费订单应经通道支付成功，实际 Paid=false（%s）", res.PaymentError)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.OrderID})
	if err != nil {
		t.Fatalf("读订单失败: %v", err)
	}
	if detail.Head.PaymentMethod != "paypal" {
		t.Fatalf("收费订单必须走真实通道（paypal），实际 %q —— 0 元分支不能误伤付费单",
			detail.Head.PaymentMethod)
	}
	if detail.Head.Total != 2500 {
		t.Fatalf("订单总额应为 2500 分，实际 %d", detail.Head.Total)
	}

	if res.Cookie == "" {
		t.Fatal("结算成功后必须返回可回写的空购物车 cookie")
	}
	after, err := f.cart.View(ctx, &cartdto.CartViewReq{ProjectID: f.projectID, Cookie: res.Cookie})
	if err != nil {
		t.Fatalf("读结算后的购物车失败: %v", err)
	}
	if !after.Empty {
		t.Fatalf("结算后购物车应为空，实际 %d 行", after.LineCount)
	}
}
