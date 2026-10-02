package feature

// order_return_account_test.go — 退货与取消的**库存 / 金额账**（批次4-A：BIZ-01 / 02 / 04 / 06 / 07 / 09）。
//
// 这六条是同一个根因的不同出口：**系统里没有一本「这件商品已经实际归还了多少」的账**。
// 于是：
//   · 退货「全退」判定拿「已申请数量」当「已退货数量」（BIZ-01）；
//   · 取消订单按订单项原始数量全额归还，不看已经退过多少（BIZ-02）；
//   · 收货前置校验只看退货单状态，不看订单已经取消/退款（BIZ-04）；
//   · 退款与收尾分两个事务，重试路径补不上（BIZ-06）；
//   · 未发货的「退款」不归还库存（BIZ-07）；
//   · 消费额从不扣减退款（BIZ-09）。
//
// 每条都断言**可观察的量**（库存件数、订单状态、退货单状态、消费额分），
// 而不是「某个函数被调用过」—— 这些缺陷的表现全是不报错的数字错。

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// orderLine 一次下单的一行（变体 + 件数）。
type orderLine struct {
	variantID string
	quantity  int
}

// paidOrderWithLines 建一张含多行的订单并付款，返回订单 id / 访客 id / 「变体 → 订单项 id」。
//
// 多行是 BIZ-01 的必要条件：单行订单里「提了申请」与「已收货」两种口径分不出差别，
// 而两张申请分属不同订单项时才会暴露「全退」判定把在途申请当成已退。
func (f *orderFixture) paidOrderWithLines(t *testing.T, email string, lines []orderLine) (orderID, userID uint64, itemIDs map[string]uint64) {
	t.Helper()
	ctx := context.Background()
	if len(lines) == 0 {
		t.Fatal("至少要有一行商品")
	}
	items := make([]orderdto.OrderItemReq, 0, len(lines))
	for _, l := range lines {
		items = append(items, orderdto.OrderItemReq{VariantID: l.variantID, Quantity: l.quantity})
	}
	req := f.createBaseReq(lines[0].variantID, lines[0].quantity)
	req.CustomerEmail = email
	req.Items = items

	created, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if _, perr := f.orders.PayOrder(ctx, &orderdto.PayOrderReq{
		OrderID:            created.ID,
		PaymentMethod:      "paypal",
		PaymentMethodTitle: "PayPal（模拟）",
		TransactionID:      "TX-" + created.OrderNo,
	}); perr != nil {
		t.Fatalf("支付落账失败: %v", perr)
	}
	detail, derr := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: created.ID})
	if derr != nil || detail.Head == nil || detail.Head.UserID == nil {
		t.Fatalf("读订单详情失败: %v", derr)
	}
	byVariant := make(map[string]uint64, len(detail.Items))
	for _, it := range detail.Items {
		byVariant[it.VariantID] = it.ID
	}
	return created.ID, *detail.Head.UserID, byVariant
}

// requestReturn 提一张退货申请（带访客身份）。
func (f *orderFixture) requestReturn(t *testing.T, orderID, userID, itemID uint64, qty int) *orderdto.ReturnResp {
	t.Helper()
	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: qty}})
	req.UserID = userID
	created, err := f.orders.RequestReturn(context.Background(), req)
	if err != nil {
		t.Fatalf("提交退货申请失败: %v", err)
	}
	return created
}

// orderStatusOf 读订单当前状态。
func (f *orderFixture) orderStatusOf(t *testing.T, orderID uint64) string {
	t.Helper()
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: orderID})
	if err != nil || detail.Head == nil {
		t.Fatalf("读订单失败: %v", err)
	}
	return detail.Head.Status
}

// returnStatusOf 读退货单当前状态。
func (f *orderFixture) returnStatusOf(t *testing.T, returnID uint64) string {
	t.Helper()
	got, err := f.orders.GetReturn(context.Background(), returnID)
	if err != nil {
		t.Fatalf("读退货单失败: %v", err)
	}
	if got == nil || got.Return == nil {
		t.Fatal("退货单不存在")
	}
	return got.Return.Status
}

// TestFullReturnRequiresReceivedNotRequested BIZ-01：**申请在途**不算已退，全退判定必须按已收货数量。
//
// 触发序列：订单含 A、B 各 1 件 → 提两张申请（都 requested）→ 只对申请 1 同意并收货。
// 期望：订单**仍是 paid**（A 的货回来了，B 的申请还只是申请）；
// 修复前：sums 由「已申请」算出 ⇒ sums[A]=1、sums[B]=1 ⇒ full ⇒ 整单被判已退款，
// 申请 2 之后收货时入库成功但退款报「已退款」，退货单永远停在 received。
func TestFullReturnRequiresReceivedNotRequested(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vidA := f.addProduct(t, "全退判定-A", 100, 5)
	_, vidB := f.addProduct(t, "全退判定-B", 60, 5)
	ctx := context.Background()
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "full-judge@example.com", []orderLine{
		{variantID: vidA, quantity: 1},
		{variantID: vidB, quantity: 1},
	})

	// 两张申请：客户把两件都申请了，而仓库只收到第一件。
	retA := f.requestReturn(t, orderID, userID, itemIDs[vidA], 1)
	retB := f.requestReturn(t, orderID, userID, itemIDs[vidB], 1)

	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: retA.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("申请 1 同意并收货失败: %v", err)
	}

	if got := f.orderStatusOf(t, orderID); got != ordermodel.OrderStatusPaid {
		t.Fatalf("只收到 1 件中的 A，订单不该进终态（应仍是 paid），实际 %s —— "+
			"「全退」判定把还在申请中的 B 也算成了已退", got)
	}

	// 申请 2 仍然必须能走完：同意 → 收货 → 入库 → 退款。
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: retB.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("申请 2 同意并收货失败（修复前会卡在 received 并报「已退款」）: %v", err)
	}
	if got := f.returnStatusOf(t, retB.ID); got != ordermodel.ReturnStatusCompleted {
		t.Fatalf("申请 2 应能完成，实际 %s", got)
	}
	if got := f.stockOf(t, vidA); got != 5 {
		t.Fatalf("A 的库存应回到 5，实际 %d", got)
	}
	if got := f.stockOf(t, vidB); got != 5 {
		t.Fatalf("B 的库存应回到 5，实际 %d", got)
	}
	if got := f.orderStatusOf(t, orderID); got != ordermodel.OrderStatusRefunded {
		t.Fatalf("两件都收货后订单才应是已退款，实际 %s", got)
	}
}

// TestCancelAfterPartialReturnRestocksOnlyUnreturned BIZ-02：取消归还量要扣掉已实际归还的部分。
//
// 触发序列：买 2 件 → 退 1 件并收货完成（订单仍 paid）→ 再取消订单。
// 期望：库存净变化为 0（下单 −2、退货 +1、取消 +1）。
// 修复前：取消按订单项原始数量归还 2 件 ⇒ 净多 1 件。
func TestCancelAfterPartialReturnRestocksOnlyUnreturned(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "取消扣已退", 30, 10)
	ctx := context.Background()
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "cancel-after-partial@example.com",
		[]orderLine{{variantID: vid, quantity: 2}})

	if got := f.stockOf(t, vid); got != 8 {
		t.Fatalf("下单后库存应为 8，实际 %d", got)
	}

	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("部分退货收货失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 9 {
		t.Fatalf("部分退货入库后库存应为 9，实际 %d", got)
	}
	if got := f.orderStatusOf(t, orderID); got != ordermodel.OrderStatusPaid {
		t.Fatalf("部分退货不该改订单状态，实际 %s", got)
	}

	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{OrderID: orderID, Reason: "客户不要了"}); err != nil {
		t.Fatalf("取消订单失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("取消后库存应回到 10（只有 1 件还没归还），实际 %d —— 重复归还了已退货的那 1 件", got)
	}
}

// TestReceiveRejectedWhenOrderNotReturnable BIZ-04：订单已取消/已退款时不许再收货。
//
// 触发序列：已付订单 → 申请退货（requested）→ 后台取消订单（库存已全额归还）→ 对该申请收货。
// 期望：收货被拒（订单维度不可退），退货单停在 approved、库存不再变动。
// 修复前：入库成功（第二次归还）→ 退款因订单已 cancelled 报错 → 退货单停在 received。
func TestReceiveRejectedWhenOrderNotReturnable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "在途申请取消", 50, 10)
	ctx := context.Background()
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "cancel-inflight@example.com",
		[]orderLine{{variantID: vid, quantity: 1}})

	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{OrderID: orderID, Reason: "客户改主意"}); err != nil {
		t.Fatalf("取消订单失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("取消后库存应回到 10，实际 %d", got)
	}
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意"}); err != nil {
		t.Fatalf("同意退货失败: %v", err)
	}

	_, err := f.orders.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{ReturnID: ret.ID})
	if err == nil {
		t.Fatal("订单已取消时收货必须被拒绝 —— 放行等于同一批货归还两次")
	}
	if !strings.Contains(err.Error(), orderenums.ErrReturnOrderNotReturnable) {
		t.Fatalf("应以「订单当前状态不可退」拒绝，实际错误 %v", err)
	}
	if got := f.returnStatusOf(t, ret.ID); got != ordermodel.ReturnStatusApproved {
		t.Fatalf("被拒的收货不该推进退货单状态（应停在 approved），实际 %s", got)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("被拒的收货不该动库存，实际 %d", got)
	}
}

// TestFullReturnReceiveIsRetryable BIZ-06：退款已完成、只差收尾的存量单必须能补完。
//
// 构造的是修复前那次中断留下的状态：订单已 refunded、退货单停在 received
// （退款提交成功但置 completed 的写入失败）。期望重试能把它收尾成 completed，
// 而不是报「已退款」让它永远停在那里。
func TestFullReturnReceiveIsRetryable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退款重试", 70, 10)
	ctx := context.Background()
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "refund-retry@example.com",
		[]orderLine{{variantID: vid, quantity: 1}})

	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("全额退货收货失败: %v", err)
	}
	if got := f.returnStatusOf(t, ret.ID); got != ordermodel.ReturnStatusCompleted {
		t.Fatalf("正常路径应完成，实际 %s", got)
	}
	stockBefore := f.stockOf(t, vid)

	// 构造中断现场：退货单退回 received（订单已是 refunded，不再回退）。
	if err := f.db.Exec("UPDATE order_returns SET status = ?, refunded_at = NULL, update_time = now() WHERE id = ?",
		ordermodel.ReturnStatusReceived, ret.ID).Error; err != nil {
		t.Fatalf("构造中断现场失败: %v", err)
	}

	if _, err := f.orders.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{ReturnID: ret.ID}); err != nil {
		t.Fatalf("重试必须只补收尾（退款已完成），实际报错: %v", err)
	}
	if got := f.returnStatusOf(t, ret.ID); got != ordermodel.ReturnStatusCompleted {
		t.Fatalf("重试后应补成 completed，实际 %s", got)
	}
	if got := f.stockOf(t, vid); got != stockBefore {
		t.Fatalf("重试不该再动库存：%d → %d", stockBefore, got)
	}
}

// TestUnshippedRefundRestocksStock BIZ-07（方案 A）：未发货订单的退款要归还库存。
//
// 同一张未发货订单上「取消」与「退款」两个按钮必须产生同样的库存结果 ——
// 否则运营点哪个按钮决定了库存对不对，而页面上两个按钮看不出区别。
// 已发货的退款**不归还**（货已经出库，要回来必须经退货入库验收），这条同时钉住。
func TestUnshippedRefundRestocksStock(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, paidVid := f.addProduct(t, "未发货退款", 45, 10)
	_, shippedVid := f.addProduct(t, "已发货退款", 45, 10)
	ctx := context.Background()

	// ① 未发货（paid）直接退款 → 货从未出库，库存必须回来。
	paidOrderID, _, _ := f.paidOrderWithLines(t, "refund-unshipped@example.com", []orderLine{{variantID: paidVid, quantity: 2}})
	if got := f.stockOf(t, paidVid); got != 8 {
		t.Fatalf("下单后库存应为 8，实际 %d", got)
	}
	if err := f.orders.RefundOrder(ctx, &orderdto.RefundOrderReq{OrderID: paidOrderID, Reason: "后台退款"}); err != nil {
		t.Fatalf("未发货订单退款失败: %v", err)
	}
	if got := f.stockOf(t, paidVid); got != 10 {
		t.Fatalf("未发货退款后库存应回到 10，实际 %d —— 退款与取消对同一状态给出了不同的库存结果", got)
	}

	// ② 已发货（shipped）退款 → 不归还（退货入库是唯一入口）。
	shippedOrderID, _, _ := f.paidOrderWithLines(t, "refund-shipped@example.com", []orderLine{{variantID: shippedVid, quantity: 1}})
	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{OrderID: shippedOrderID, ToStatus: ordermodel.OrderStatusShipped}); err != nil {
		t.Fatalf("发货失败: %v", err)
	}
	if err := f.orders.RefundOrder(ctx, &orderdto.RefundOrderReq{OrderID: shippedOrderID, Reason: "后台退款"}); err != nil {
		t.Fatalf("已发货订单退款失败: %v", err)
	}
	if got := f.stockOf(t, shippedVid); got != 9 {
		t.Fatalf("已发货退款不该归还库存（货已出库），实际 %d", got)
	}
}

// TestPartialReturnReducesSpentTotal BIZ-09：部分退货要回冲消费额。
//
// 触发序列：付 1000 元（两件各 500）→ 退其中 1 件并收货 → 订单仍 paid。
// 期望消费额 = 500 元；修复前按 1000 元计入 ⇒ 凭已经退回去的钱升档。
func TestPartialReturnReducesSpentTotal(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "消费额回冲", 500, 10)
	ctx := context.Background()
	// 500 元 × 2 件 = 100000 分。
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "spent-refund@example.com",
		[]orderLine{{variantID: vid, quantity: 2}})

	totals, err := f.orderModel.SpentTotalsByProject(ctx, f.projectID)
	if err != nil {
		t.Fatalf("读消费额失败: %v", err)
	}
	if got := totals[userID]; got != 100000 {
		t.Fatalf("刚付款时应按 100000 分计入，实际 %d", got)
	}

	// 退 1 件（50000 分）并收货：订单仍是 paid，钱已经退回去一半。
	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("部分退货收货失败: %v", err)
	}

	totals, err = f.orderModel.SpentTotalsByProject(ctx, f.projectID)
	if err != nil {
		t.Fatalf("读消费额失败: %v", err)
	}
	if got := totals[userID]; got != 50000 {
		t.Fatalf("退款 50000 分后消费额应为 50000，实际 %d —— 退回去的钱仍在累计消费里", got)
	}
}

// TestSpentTotalsConsistentAcrossPaths 同一笔净消费在两条口径路径下必须是同一个数。
//
// 两条路径是同一件事的两种视图：客户页摘要（SummaryByUser，单客户 + 最近一单）
// 与会员候选聚合（SpentTotalsByProject，全工程按人分组）。它们各写一份口径时的失败模式是
// 「会员按净额分档、客户页按总额显示」—— 两个数字都不报错，只在有人对账时才发现。
// 这条用例的价值就是**防两处再次分叉**：把两条路径的值直接对比，任一处的表达式被改坏都会红。
func TestSpentTotalsConsistentAcrossPaths(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "口径一致", 500, 10)
	ctx := context.Background()
	// 500 元 × 2 件 = 100000 分。
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "spent-consistent@example.com",
		[]orderLine{{variantID: vid, quantity: 2}})

	assertSameNet := func(stage string, want int64) {
		t.Helper()
		totals, err := f.orderModel.SpentTotalsByProject(ctx, f.projectID)
		if err != nil {
			t.Fatalf("%s：读会员候选消费额失败: %v", stage, err)
		}
		summary, serr := f.orderModel.SummaryByUser(ctx, f.projectID, userID)
		if serr != nil {
			t.Fatalf("%s：读客户页摘要失败: %v", stage, serr)
		}
		if summary.TotalAmount != totals[userID] {
			t.Fatalf("%s：两处口径分叉 —— 客户页 %d / 会员候选 %d",
				stage, summary.TotalAmount, totals[userID])
		}
		if summary.TotalAmount != want {
			t.Fatalf("%s：净消费额应为 %d，实际 %d", stage, want, summary.TotalAmount)
		}
	}

	assertSameNet("刚付款", 100000)

	// 退 1 件（50000 分）并收货：订单仍是 paid，两边都要扣掉这 50000。
	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("部分退货收货失败: %v", err)
	}
	assertSameNet("部分退货后", 50000)
}
