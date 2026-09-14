package feature

// order_concurrency_test.go — 订单域的并发验收（TX-016）。
//
// 为什么单列一个文件：本次审计发现的并发缺陷多数落在订单域（TX-002 每人限次超发、
// TX-003 超退、TX-006 已取消单收到支付回调、TX-010 整单退款误判），而库存域早有
// 40 并发扣减的测试、采购收货与发布激活也各有并发测试 —— 订单域的并发路径此前
// 一条测试都没有。实现「看起来是对的」不等于被证明过。
//
// 每条用例对应一个**已经修掉的**缺陷，作为修复的验收标准（用例写法即回归测试）：
//   · 并发发货   → 只成功一次，流转链里 shipped 只出现一次；
//   · 并发支付   → 只落账一次，其余走幂等分支返回同一结果；
//   · 取消与支付并发 → 最终状态唯一、库存只归还一次、日志与状态自洽；
//   · 并发退货申请 → 不超退（TX-003）；
//   · 同用户并发用券 → 不超发（TX-002）。
//
// 全部用 -race 跑：go test -race ./public/test/order/...

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

// runConcurrently 并发跑 n 个任务，返回成功数、失败数与失败原因。
//
// 每个任务拿到自己的序号（用来造不同的幂等键），错误不中断其他任务 ——
// 并发用例要的是「分布」而不是「第一个失败就把其他 goroutine 掐掉」。
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

// countLogs 统计流转链里「真正进入某状态」的次数。
//
// 只数 FromStatus != ToStatus 的条目：同状态的记录是**留痕**而不是状态变化 ——
// 重复的支付回调撞上终态（订单已取消 / 已退款）会写一条 cancelled→cancelled 的
// 「待人工核对」，库存归还未完成也会留一条；它们说明「发生过什么」，
// 不代表状态又变了一次。把留痕算进流转次数，就会把正确的行为判成缺陷。
func countLogs(detail *orderdto.OrderDetailResp, toStatus string) int {
	n := 0
	for _, log := range detail.Logs {
		if log.ToStatus == toStatus && log.FromStatus != toStatus {
			n++
		}
	}
	return n
}

// TestConcurrentShipOnlyOneSucceeds 并发发货只该成功一次。
//
// 两次「发货」若都在行锁外读到 paid，就都会判定合法，结果是两条流转记录 ——
// 客服看到两次发货、仓库可能发两次货。行锁把判定与写入圈在同一个事务里，
// 这里就是那条不变量的验收。
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

// TestConcurrentPayOrderLandsOnce 并发支付回调只落账一次，其余幂等返回。
//
// 网关的通知会重发、访客会连点两次：重复到达必须返回与第一次**相同的结果**，
// 而不是报「状态不支持」—— 报错会让网关一直重试，也让用户第二次点击变成报错弹窗。
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

// TestConcurrentCancelAndPayKeepSingleOutcome 取消与支付并发：结局唯一、库存只动一次。
//
// 这是最像真实事故的一条：客户在页面上点「取消」，同一时刻网关的支付成功通知到了。
// 要守住三件事：
//   ① 最终状态唯一（要么 cancelled 要么 paid，不能既取消又收款还看不出所以然）；
//   ② 库存只归还一次（并发取消各还一次 = 商家凭空多出库存）；
//   ③ 支付回调撞上终态时**不报错**（钱已扣，对网关报错等于让它无限重试）。
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
	if got := f.stockOf(t, vid); got != 9 {
		t.Fatalf("建单后库存应为 9，实际 %d", got)
	}

	const workers = 8
	// 两类调用的失败语义不同，必须分开看：
	//   · 支付回调**不许报错** —— 网关会重发，报错等于让它无限重试（钱已经扣了）；
	//   · 重复取消**允许报错**（「订单已取消，不能再操作」）—— 这不是回调，
		// 用户第二次点取消得到一个明确结论比拿到一个静默的成功更正确。
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

	// 库存：取消则归还（回到 10），未取消则保持扣减（9）。
	// 出现 11 说明并发取消各归还了一次 —— 那是商家的净损失。
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

	// 日志与状态自洽：终态各自最多出现一次，且最后一条流转落在最终状态上。
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

// TestConcurrentReturnRequestsDoNotOverReturn 并发退货申请不超退（TX-003 验收）。
//
// 缺陷原形：可退数量在事务外先读再写，N 个并发申请各自都读到「还能退 3 件」，
// 于是落 N 张各退 3 件的单 —— 货只发了 3 件，却退了 3N 件。
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

	// 已退额度必须刚好用尽：再退一件应被拒。
	again := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	again.UserID = userID
	again.RequestID = "return-race-after"
	if _, err := f.orders.RequestReturn(ctx, again); err == nil {
		t.Fatal("额度用尽后仍能申请退货：并发下超退了")
	}
}

// TestConcurrentCouponPerUserLimitNotExceeded 同用户并发用券不超发（TX-002 验收）。
//
// 缺陷原形：每人限次在事务外先读再写，同一账号并发下单时各自都读到「还没用过」，
// 于是一张「每人一次」的券被用掉多次。判定与核销必须在同一个事务里、同一把行锁下。
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

	// 先下一单拿到访客账号：并发建单时显式带上 userID，避免把「同时开号」的竞态
	// 混进这条用例（那是另一条链路，失败了会让人误判用券逻辑）。
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

	// 核销记录与计数都必须恰好 1（两者任一超了都是超发）。
	res, err := f.orders.ListCouponRedemptions(ctx, &orderdto.CouponRedemptionListReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("读核销记录失败: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("核销记录应恰好 1 条，实际 %d 条", res.Total)
	}
}
