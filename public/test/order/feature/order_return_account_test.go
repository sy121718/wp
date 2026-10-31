package feature

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

type orderLine struct {
	variantID string
	quantity  int
}

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

func (f *orderFixture) requestReturn(t *testing.T, orderID, userID, itemID uint64, qty int) *orderdto.VisitorReturnResp {
	t.Helper()
	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: qty}})
	req.UserID = userID
	created, err := f.orders.RequestReturn(context.Background(), req)
	if err != nil {
		t.Fatalf("提交退货申请失败: %v", err)
	}
	return created
}

func (f *orderFixture) orderStatusOf(t *testing.T, orderID uint64) string {
	t.Helper()
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: orderID})
	if err != nil || detail.Head == nil {
		t.Fatalf("读订单失败: %v", err)
	}
	return detail.Head.Status
}

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

	retA := f.requestReturn(t, orderID, userID, itemIDs[vidA], 1)
	retB := f.requestReturn(t, orderID, userID, itemIDs[vidB], 1)

	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: retA.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("申请 1 同意并收货失败: %v", err)
	}

	if got := f.orderStatusOf(t, orderID); got != ordermodel.OrderStatusPaid {
		t.Fatalf("只收到 1 件中的 A，订单不该进终态（应仍是 paid），实际 %s —— "+
			"「全退」判定把还在申请中的 B 也算成了已退", got)
	}

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

func TestCancelAfterPartialReturnRestocksOnlyUnreturned(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "取消扣已退", 30, 10)
	ctx := context.Background()
	orderID, userID, itemIDs := f.paidOrderWithLines(t, "cancel-after-partial@example.com",
		[]orderLine{{variantID: vid, quantity: 2}})

	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("下单后预占应为 2，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("下单不得扣减在库数量，期望 10，实际 %d", got)
	}

	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("部分退货收货失败: %v", err)
	}
	if got := f.reservedOf(t, vid); got != 1 {
		t.Fatalf("部分退货后预占应减到 1，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("未发货的退货不改变在库数量，期望 10，实际 %d", got)
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

func TestUnshippedRefundRestocksStock(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, paidVid := f.addProduct(t, "未发货退款", 45, 10)
	_, shippedVid := f.addProduct(t, "已发货退款", 45, 10)
	ctx := context.Background()

	paidOrderID, _, _ := f.paidOrderWithLines(t, "refund-unshipped@example.com", []orderLine{{variantID: paidVid, quantity: 2}})
	if got := f.reservedOf(t, paidVid); got != 2 {
		t.Fatalf("下单后预占应为 2，实际 %d", got)
	}
	if err := f.orders.RefundOrder(ctx, &orderdto.RefundOrderReq{OrderID: paidOrderID, Reason: "后台退款"}); err != nil {
		t.Fatalf("未发货订单退款失败: %v", err)
	}

	if got := f.reservedOf(t, paidVid); got != 0 {
		t.Fatalf("未发货退款后预占应归零，实际 %d", got)
	}
	if got := f.stockOf(t, paidVid); got != 10 {
		t.Fatalf("未发货退款后库存应仍是 10，实际 %d", got)
	}

	shippedOrderID, _, _ := f.paidOrderWithLines(t, "refund-shipped@example.com", []orderLine{{variantID: shippedVid, quantity: 1}})
	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{OrderID: shippedOrderID, ToStatus: ordermodel.OrderStatusShipped}); err != nil {
		t.Fatalf("发货失败: %v", err)
	}
	if err := f.orders.RefundOrder(ctx, &orderdto.RefundOrderReq{OrderID: shippedOrderID, Reason: "后台退款"}); err != nil {
		t.Fatalf("已发货订单退款失败: %v", err)
	}

	if got := f.stockOf(t, shippedVid); got != 10 {
		t.Fatalf("已发货退款不该归还库存（货已出库），实际 %d", got)
	}
}

func TestPartialReturnReducesSpentTotal(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "消费额回冲", 500, 10)
	ctx := context.Background()

	orderID, userID, itemIDs := f.paidOrderWithLines(t, "spent-refund@example.com",
		[]orderLine{{variantID: vid, quantity: 2}})

	totals, err := f.orderModel.SpentTotalsByProject(ctx, f.projectID)
	if err != nil {
		t.Fatalf("读消费额失败: %v", err)
	}
	if got := totals[userID]; got != 100000 {
		t.Fatalf("刚付款时应按 100000 分计入，实际 %d", got)
	}

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

func TestSpentTotalsConsistentAcrossPaths(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "口径一致", 500, 10)
	ctx := context.Background()

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

	ret := f.requestReturn(t, orderID, userID, itemIDs[vid], 1)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: ret.ID, Remark: "同意", AutoReceive: true}); err != nil {
		t.Fatalf("部分退货收货失败: %v", err)
	}
	assertSameNet("部分退货后", 50000)
}
