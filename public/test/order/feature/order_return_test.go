package feature

// order_return_test.go — 退货入库的用例链路测试（BIZ-1）。
//
// 覆盖的是**不变量**而不是「方法能跑通」：
//   · 归属：访客只能退自己的单（别人的单与不存在的单返回同一句话）；
//   · 额度：累计可退数量不能被超（部分退货是一等公民）；
//   · 顺序：**先入库、后退款** —— 入库失败时不进退款；
//   · 幂等：重复确认收货不会把同一批货加两遍库存；
//   · 全额 vs 部分：全额才把订单转成已退款，部分退货不动订单状态。

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// paidOrder 建单 → 付款 → 返回订单 id / 访客账号 id / 第一个订单项 id 与数量。
//
// 退货只对**已付款**的订单开放（没付钱的单没有可退的钱），所以这里必须真的走一次支付落账。
func (f *orderFixture) paidOrder(t *testing.T, email string, variantID string, qty int) (orderID, userID, itemID uint64) {
	t.Helper()
	ctx := context.Background()
	req := f.createBaseReq(variantID, qty)
	req.CustomerEmail = email
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
	detail, derr := f.orders.GetOrder(ctx, created.ID)
	if derr != nil || detail.Head == nil || detail.Head.UserID == nil || len(detail.Items) == 0 {
		t.Fatalf("读订单详情失败: %v / %+v", derr, detail)
	}
	return created.ID, *detail.Head.UserID, detail.Items[0].ID
}

// returnReq 造一个退货申请请求。
func (f *orderFixture) returnReq(orderID uint64, items []orderdto.ReturnItemReq) *orderdto.ReturnRequestReq {
	return &orderdto.ReturnRequestReq{
		ProjectID: f.projectID,
		OrderID:   orderID,
		Items:     items,
		Reason:    "尺码不合适",
	}
}

// TestReturnRequestScopedToOwnOrder 访客只能退自己的单：别人的单与不存在的单同一句话。
func TestReturnRequestScopedToOwnOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货归属商品", 50, 10)
	orderID, _, itemID := f.paidOrder(t, "return-owner@example.com", vid, 1)
	ctx := context.Background()

	// 别人（另一个 userID）来退：必须失败，且原因与「不存在」完全一致。
	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = 999999
	if _, err := f.orders.RequestReturn(ctx, req); err == nil || !strings.Contains(err.Error(), orderenums.ErrOrderNotFound) {
		t.Fatalf("退别人的单应表现为「订单不存在」，实际: %v", err)
	}
	// 没有身份（UserID=0）一律拒绝 —— 「不传即放行」是这里最危险的默认值。
	req.UserID = 0
	if _, err := f.orders.RequestReturn(ctx, req); err == nil {
		t.Fatal("缺少 userID 的退货申请必须被拒绝")
	}
}

// TestReturnRequestRespectsReturnableQuantity 累计可退数量不能被超（含部分退货）。
func TestReturnRequestRespectsReturnableQuantity(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货额度商品", 30, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-quota@example.com", vid, 3)
	ctx := context.Background()

	first := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}})
	first.UserID = userID
	if _, err := f.orders.RequestReturn(ctx, first); err != nil {
		t.Fatalf("首次申请 2 件应成功: %v", err)
	}
	// 只剩 1 件可退：再申请 2 件必须被拒。
	second := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}})
	second.UserID = userID
	if _, err := f.orders.RequestReturn(ctx, second); err == nil || !strings.Contains(err.Error(), orderenums.ErrReturnQuantityExceeded) {
		t.Fatalf("超出可退数量应被拒绝，实际: %v", err)
	}
	// 剩下这 1 件可以退。
	third := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	third.UserID = userID
	if _, err := f.orders.RequestReturn(ctx, third); err != nil {
		t.Fatalf("剩余 1 件应可退: %v", err)
	}
	// 第四张单一律拒绝（额度已用尽）。
	fourth := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	fourth.UserID = userID
	if _, err := f.orders.RequestReturn(ctx, fourth); err == nil {
		t.Fatal("额度用尽后必须拒绝")
	}
}

// TestReturnRequestIsIdempotentByRequestID 同一幂等键只落一张申请单。
func TestReturnRequestIsIdempotentByRequestID(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货幂等商品", 20, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-idem@example.com", vid, 2)
	ctx := context.Background()

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	req.RequestID = "ret-req-1"
	first, err := f.orders.RequestReturn(ctx, req)
	if err != nil {
		t.Fatalf("首次申请失败: %v", err)
	}
	second, err := f.orders.RequestReturn(ctx, req)
	if err != nil {
		t.Fatalf("重复提交应返回既有单而不是报错: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("幂等键命中应返回同一张单，实际 %d / %d", first.ID, second.ID)
	}
	list, err := f.orders.ListVisitorReturns(ctx, &orderdto.VisitorReturnListReq{ProjectID: f.projectID, UserID: userID})
	if err != nil {
		t.Fatalf("读访客退货列表失败: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("应只有一张申请单，实际 %d", list.Total)
	}
}

// TestReturnApproveAndReceiveReturnsStockAndRefund 一步到底：入库 + 退款 + 库存回补。
//
// 这条是「货真的回来了才退钱」的可执行说明：只有买入的 2 件全部退回之后，
// 库存才回到原值、订单才变成已退款。
func TestReturnApproveAndReceiveReturnsStockAndRefund(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货入库商品", 88, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-flow@example.com", vid, 2)
	ctx := context.Background()
	if got := f.stockOf(t, vid); got != 8 {
		t.Fatalf("下单后库存应为 8，实际 %d", got)
	}

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}})
	req.UserID = userID
	created, err := f.orders.RequestReturn(ctx, req)
	if err != nil {
		t.Fatalf("提交申请失败: %v", err)
	}
	if created.Status != ordermodel.ReturnStatusRequested {
		t.Fatalf("新申请应为待审核，实际 %s", created.Status)
	}
	// 88 元 × 2 = 17600 分
	if created.RefundAmount != 17600 {
		t.Fatalf("退款额应为 17600 分，实际 %d", created.RefundAmount)
	}

	// 同意并立即完成入库 + 退款。
	done, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{
		ReturnID: created.ID, Remark: "同意", AutoReceive: true, OperatorName: "客服A",
	})
	if err != nil {
		t.Fatalf("同意并收货失败: %v", err)
	}
	if done.Status != ordermodel.ReturnStatusCompleted {
		t.Fatalf("应一步完成，实际状态 %s", done.Status)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("退货入库后库存应回到 10，实际 %d", got)
	}
	detail, _ := f.orders.GetOrder(ctx, orderID)
	if detail.Head.Status != ordermodel.OrderStatusRefunded {
		t.Fatalf("全额退货后订单应为已退款，实际 %s", detail.Head.Status)
	}
}

// TestReturnReceiveIsIdempotent 重复确认收货：库存只加一次。
//
// 入库的 ChangeStock **没有幂等键** —— 门闩（approved → received 只放行一次）是唯一的护栏，
// 所以这条必须真的验证「第二次点击不会再加一遍库存」。
func TestReturnReceiveIsIdempotent(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货重复商品", 40, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-again@example.com", vid, 1)
	ctx := context.Background()

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	created, _ := f.orders.RequestReturn(ctx, req)
	if _, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: created.ID, Remark: "同意"}); err != nil {
		t.Fatalf("同意失败: %v", err)
	}

	first, err := f.orders.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{ReturnID: created.ID})
	if err != nil {
		t.Fatalf("首次收货失败: %v", err)
	}
	if first.Status != ordermodel.ReturnStatusCompleted {
		t.Fatalf("首次收货后应完成，实际 %s", first.Status)
	}
	stockAfterFirst := f.stockOf(t, vid)
	if stockAfterFirst != 10 {
		t.Fatalf("入库后库存应为 10，实际 %d", stockAfterFirst)
	}

	second, err := f.orders.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{ReturnID: created.ID})
	if err != nil {
		t.Fatalf("重复收货不该报错（重试是常态）: %v", err)
	}
	if second.Status != ordermodel.ReturnStatusCompleted {
		t.Fatalf("重复收货后状态应仍是已完成，实际 %s", second.Status)
	}
	if got := f.stockOf(t, vid); got != stockAfterFirst {
		t.Fatalf("重复收货把同一批货加了两次：%d → %d", stockAfterFirst, got)
	}
}

// TestReturnReceiveRequiresApproval 未同意就收货：拒绝，且不动库存。
func TestReturnReceiveRequiresApproval(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货未审商品", 25, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-unapproved@example.com", vid, 1)
	ctx := context.Background()

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	created, _ := f.orders.RequestReturn(ctx, req)

	_, err := f.orders.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{ReturnID: created.ID})
	if err == nil || !strings.Contains(err.Error(), orderenums.ErrReturnNotReceivable) {
		t.Fatalf("未同意的申请不能收货，实际: %v", err)
	}
	if got := f.stockOf(t, vid); got != 9 {
		t.Fatalf("被拒的收货不该动库存，实际 %d", got)
	}
}

// TestReturnRejectRequiresReason 拒绝必须给理由（否则客户只会再申请一次）。
func TestReturnRejectRequiresReason(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货拒绝商品", 60, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-reject@example.com", vid, 1)
	ctx := context.Background()

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	created, _ := f.orders.RequestReturn(ctx, req)

	if _, err := f.orders.RejectReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: created.ID}); err == nil || !strings.Contains(err.Error(), orderenums.ErrReturnRejectReasonRequired) {
		t.Fatalf("没有理由的拒绝应被拦住，实际: %v", err)
	}
	rejected, err := f.orders.RejectReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: created.ID, Remark: "超出七天无理由期限"})
	if err != nil {
		t.Fatalf("带理由的拒绝应成功: %v", err)
	}
	if rejected.Status != ordermodel.ReturnStatusRejected {
		t.Fatalf("状态应为已拒绝，实际 %s", rejected.Status)
	}
	// 被拒之后额度要还回来（客户可以换个理由再申请）。
	again := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	again.UserID = userID
	if _, err := f.orders.RequestReturn(ctx, again); err != nil {
		t.Fatalf("被拒后应可重新申请: %v", err)
	}
}

// TestReturnPartialRefundKeepsOrderStatus 部分退货：订单状态不动（还有没退的货）。
func TestReturnPartialRefundKeepsOrderStatus(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "部分退货商品", 70, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-partial@example.com", vid, 3)
	ctx := context.Background()

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	created, _ := f.orders.RequestReturn(ctx, req)
	res, err := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{ReturnID: created.ID, Remark: "同意", AutoReceive: true})
	if err != nil {
		t.Fatalf("部分退货应能完成: %v", err)
	}
	if res.Status != ordermodel.ReturnStatusCompleted {
		t.Fatalf("退货单应完成，实际 %s", res.Status)
	}
	if got := f.stockOf(t, vid); got != 8 {
		t.Fatalf("退回 1 件后库存应为 8（10-3+1），实际 %d", got)
	}
	detail, _ := f.orders.GetOrder(ctx, orderID)
	if detail.Head.Status == ordermodel.OrderStatusRefunded {
		t.Fatal("部分退货不该把整单标成已退款 —— 还有没退的货，财务会因此对不上账")
	}
	// 但流转链上要留下「退了多少钱」的痕迹（对账看的是这个，不是只看状态列）。
	found := false
	for _, lg := range detail.Logs {
		if strings.Contains(lg.Remark, "部分退货退款") {
			found = true
		}
	}
	if !found {
		t.Fatalf("部分退货应在订单流转链上留痕，实际日志: %+v", detail.Logs)
	}
}

// TestReturnCancelOnlyBeforeReview 撤销：审核前可以，审核后不行。
func TestReturnCancelOnlyBeforeReview(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "撤销退货商品", 15, 10)
	orderID, userID, itemID := f.paidOrder(t, "return-cancel@example.com", vid, 1)
	ctx := context.Background()

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	created, _ := f.orders.RequestReturn(ctx, req)

	// 撤销别人的申请：拒绝（与不存在同一句话）。
	if err := f.orders.CancelReturn(ctx, &orderdto.ReturnCancelReq{ReturnID: created.ID, UserID: 888888}); err == nil || !strings.Contains(err.Error(), orderenums.ErrReturnNotFound) {
		t.Fatalf("撤别人的申请应被拒绝，实际: %v", err)
	}
	if err := f.orders.CancelReturn(ctx, &orderdto.ReturnCancelReq{ReturnID: created.ID, UserID: userID, Reason: "不退了"}); err != nil {
		t.Fatalf("自己的待审核申请应可撤销: %v", err)
	}
	got, _ := f.orders.GetReturn(ctx, created.ID)
	if got.Return.Status != ordermodel.ReturnStatusCancelled {
		t.Fatalf("状态应为已撤销，实际 %s", got.Return.Status)
	}

	// 撤销之后额度还回来，且不能再撤销一次。
	if err := f.orders.CancelReturn(ctx, &orderdto.ReturnCancelReq{ReturnID: created.ID, UserID: userID}); err == nil {
		t.Fatal("已撤销的申请不能再次撤销")
	}
	again := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	again.UserID = userID
	if _, err := f.orders.RequestReturn(ctx, again); err != nil {
		t.Fatalf("撤销后应可重新申请: %v", err)
	}
}

// TestReturnRequestRejectedForUnpaidOrder 未付款的订单不能申请退货。
func TestReturnRequestRejectedForUnpaidOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "未付款商品", 12, 10)
	ctx := context.Background()
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	detail, _ := f.orders.GetOrder(ctx, created.ID)
	if detail.Head.UserID == nil {
		t.Fatal("访客下单应自动开号")
	}
	req := f.returnReq(created.ID, []orderdto.ReturnItemReq{{OrderItemID: detail.Items[0].ID, Quantity: 1}})
	req.UserID = *detail.Head.UserID
	if _, err := f.orders.RequestReturn(ctx, req); err == nil || !strings.Contains(err.Error(), orderenums.ErrReturnOrderNotReturnable) {
		t.Fatalf("未付款的订单不能退货，实际: %v", err)
	}
}
