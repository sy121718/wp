package feature

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

func runConcurrently(n int, fn func(i int) error) (success int, failures []error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			err := fn(i)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, fmt.Errorf("#%d: %w", i, err))
				return
			}
			success++
		}(i)
	}
	wg.Wait()
	return success, failures
}

func countLogs(detail *orderdto.OrderDetailResp, toStatus string) int {
	n := 0
	for _, log := range detail.Logs {
		if log.ToStatus == toStatus && log.FromStatus != toStatus {
			n++
		}
	}
	return n
}

func TestConcurrentShipOnlyOneSucceeds(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "并发发货商品", 20.00, 10)
	orderID, _, _ := f.paidOrder(t, "ship-race@example.com", vid, 1)
	ctx := context.Background()

	const workers = 8
	success, failures := runConcurrently(workers, func(i int) error {
		return f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
			OrderID:      orderID,
			ToStatus:     ordermodel.OrderStatusShipped,
			Remark:       "发货",
			OperatorType: ordermodel.OperatorTypeAdmin,
		})
	})
	if success != 1 {
		t.Fatalf("%d 个并发发货只应成功 1 次，实际 %d 次（失败 %d 次）", workers, success, len(failures))
	}
	if len(failures) != workers-1 {
		t.Fatalf("其余 %d 次应全部失败，实际失败 %d 次", workers-1, len(failures))
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: orderID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusShipped {
		t.Fatalf("最终状态应为 shipped，实际 %s", detail.Head.Status)
	}
	if n := countLogs(detail, ordermodel.OrderStatusShipped); n != 1 {
		t.Fatalf("流转链里 shipped 应只出现 1 次，实际 %d 次", n)
	}
}

func TestConcurrentPayOrderLandsOnce(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "并发支付商品", 100.00, 10)
	ctx := context.Background()
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	const workers = 8
	var landed int32
	success, failures := runConcurrently(workers, func(i int) error {
		res, perr := f.orders.PayOrder(ctx, &orderdto.PayOrderReq{
			OrderID:            created.ID,
			PaymentMethod:      "paypal",
			PaymentMethodTitle: "PayPal（模拟）",
			TransactionID:      fmt.Sprintf("TX-race-%d", i),
		})
		if perr != nil {
			return perr
		}
		if !res.AlreadyPaid {
			atomic.AddInt32(&landed, 1)
		}
		return nil
	})
	if len(failures) != 0 {
		t.Fatalf("重复的支付落账应幂等返回而不是报错，实际失败 %d 次：%v", len(failures), failures)
	}
	if success != workers {
		t.Fatalf("全部 %d 次都应成功返回，实际 %d 次", workers, success)
	}
	if landed != 1 {
		t.Fatalf("真正落账的次数应为 1，实际 %d", landed)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: created.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("最终状态应为 paid，实际 %s", detail.Head.Status)
	}
	if n := countLogs(detail, ordermodel.OrderStatusPaid); n != 1 {
		t.Fatalf("流转链里 paid 应只出现 1 次，实际 %d 次", n)
	}
}

func TestConcurrentCancelAndPayKeepSingleOutcome(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "并发取消支付商品", 80.00, 10)
	ctx := context.Background()
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}

	if got := f.reservedOf(t, vid); got != 1 {
		t.Fatalf("建单后预占应为 1，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("建单不得扣减在库数量，期望 10，实际 %d", got)
	}

	const workers = 8

	var mu sync.Mutex
	var cancelSuccess int32
	var payErrors []error
	runConcurrently(workers, func(i int) error {
		if i%2 == 0 {
			_, cerr := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
				OrderID: created.ID,
				Reason:  "客户取消",
			})
			if cerr == nil {
				atomic.AddInt32(&cancelSuccess, 1)
			}
			return nil
		}
		_, perr := f.orders.PayOrder(ctx, &orderdto.PayOrderReq{
			OrderID:            created.ID,
			PaymentMethod:      "paypal",
			PaymentMethodTitle: "PayPal（模拟）",
			TransactionID:      fmt.Sprintf("TX-cancel-race-%d", i),
		})
		if perr != nil {
			mu.Lock()
			payErrors = append(payErrors, perr)
			mu.Unlock()
		}
		return nil
	})
	if len(payErrors) != 0 {
		t.Fatalf("支付落账不许对通道报错（钱已扣，报错会让网关无限重试），实际失败：%v", payErrors)
	}
	if cancelSuccess > 1 {
		t.Fatalf("取消最多成功一次，实际 %d 次", cancelSuccess)
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: created.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	switch detail.Head.Status {
	case ordermodel.OrderStatusCancelled, ordermodel.OrderStatusPaid:
	default:
		t.Fatalf("最终状态只可能是 cancelled 或 paid，实际 %s", detail.Head.Status)
	}

	stock := f.stockOf(t, vid)
	if detail.Head.Status == ordermodel.OrderStatusCancelled {
		if stock != 10 {
			t.Fatalf("取消后库存应归还到 10，实际 %d", stock)
		}
		if cancelSuccess != 1 {
			t.Fatalf("取消只该成功一次，实际 %d 次", cancelSuccess)
		}
	} else if stock != 9 {
		t.Fatalf("未取消时库存应保持 9，实际 %d", stock)
	}

	if n := countLogs(detail, ordermodel.OrderStatusCancelled); n > 1 {
		t.Fatalf("取消流转记录最多 1 条，实际 %d 条", n)
	}
	if n := countLogs(detail, ordermodel.OrderStatusPaid); n > 1 {
		t.Fatalf("支付流转记录最多 1 条，实际 %d 条", n)
	}
	if len(detail.Logs) == 0 || detail.Logs[len(detail.Logs)-1].ToStatus != detail.Head.Status {
		t.Fatalf("最后一条流转应与订单状态一致：状态 %s，最后流转 %+v", detail.Head.Status, detail.Logs[len(detail.Logs)-1])
	}
}

func TestConcurrentReturnRequestsDoNotOverReturn(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "并发退货商品", 30.00, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-race@example.com", vid, 3)
	ctx := context.Background()

	const workers = 8
	success, _ := runConcurrently(workers, func(i int) error {
		req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 3}})
		req.UserID = userID
		req.RequestID = fmt.Sprintf("return-race-%d", i)
		_, rerr := f.orders.RequestReturn(ctx, req)
		return rerr
	})
	if success != 1 {
		t.Fatalf("可退 3 件、%d 个并发申请各要 3 件，只应有 1 个成功，实际 %d 个", workers, success)
	}

	again := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	again.UserID = userID
	again.RequestID = "return-race-after"
	if _, err := f.orders.RequestReturn(ctx, again); err == nil {
		t.Fatal("额度用尽后仍能申请退货：并发下超退了")
	}
}

func TestConcurrentCouponPerUserLimitNotExceeded(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "并发用券商品", 100.00, 20)
	ctx := context.Background()
	f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "onceper", DiscountType: "percent", DiscountValue: 10,
		PerUserLimit: 1, MaxUses: 100,
	})

	first, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("首单建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: first.ID})
	if err != nil || detail.Head.UserID == nil {
		t.Fatalf("读首单详情失败: %v", err)
	}
	userID := *detail.Head.UserID

	const workers = 8
	success, _ := runConcurrently(workers, func(i int) error {
		req := f.createBaseReq(vid, 1)
		req.CustomerEmail = "coupon-race@example.com"
		req.UserID = &userID
		req.CouponCode = "onceper"
		req.RequestID = fmt.Sprintf("coupon-race-%d", i)
		_, cerr := f.orders.CreateOrder(ctx, req)
		return cerr
	})
	if success != 1 {
		t.Fatalf("每人限 1 次的券在 %d 个并发下单中只应成功 1 次，实际 %d 次", workers, success)
	}

	res, err := f.orders.ListCouponRedemptions(ctx, &orderdto.CouponRedemptionListReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读核销记录失败: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("核销记录应恰好 1 条，实际 %d 条", res.Total)
	}
}
