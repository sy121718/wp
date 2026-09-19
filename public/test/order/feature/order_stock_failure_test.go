package feature

// order_stock_failure_test.go — 故障注入：库存变动失败时，订单侧必须**整体回滚**。
//
// 三条对应「订单侧调库存」的全部路径（建单扣减 / 取消归还 / 退货入库）。
// 口径来自 AGENTS.md「写操作的事务与回滚」：同库跨模块只传 *gorm.DB 句柄，
// 失败即整体回滚 —— 不允许「先提交 A、再动库存、失败再补偿」（补偿调用本身就是吞错形态，
// 补偿再失败就留下「记了账没动库存」，事后无从分辨到底发没发货）。
//
// 注入方式是换一个**恒失败的库存端口**（ordercontract.StockOperator）：要验的正是
// 「订单侧怎么对待库存的错误」。替身把两个 Tx 方法也实现掉 —— 它们拿到的是订单的
// 真实事务，返回错误即触发回滚。

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// failingStockOperator 故障注入用的库存端口：四条方法恒失败。
type failingStockOperator struct{ err error }

func (f failingStockOperator) DeductStock(context.Context, *ordercontract.StockDeduction) error {
	return f.err
}
func (f failingStockOperator) ChangeStock(context.Context, *ordercontract.StockAdjustment) error {
	return f.err
}
func (f failingStockOperator) DeductStockTx(context.Context, *gorm.DB, *ordercontract.StockDeduction) error {
	return f.err
}
func (f failingStockOperator) ChangeStockTx(context.Context, *gorm.DB, *ordercontract.StockAdjustment) error {
	return f.err
}

// 编译期断言：替身必须实现订单侧的库存契约（契约加方法时这里立刻报错）。
var _ ordercontract.StockOperator = failingStockOperator{}

// TestOrderCreateStockFailureLeavesNothingBehind 建单扣减失败 → 订单头 / 明细 / 流水一条不留。
//
// 与「库存不足」那条互为对照：那条走真实库存（业务拒绝），这条走库存端口不可用
// （基础设施故障）。判定与呈现不同，但「不留半截状态」是同一条断言。
func TestOrderCreateStockFailureLeavesNothingBehind(t *testing.T) {
	f := newOrderFixtureWithStock(t, failingStockOperator{err: errors.New("stub: 库存服务不可用")})
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "库存故障商品", 30, 5)

	// 货是足的（5 件）且补货不经替身：失败只可能来自库存端口本身。
	_, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err == nil || err.Error() != orderenums.ErrStockUnavailable {
		t.Fatalf("库存端口不可用应报 %s，实际 %v", orderenums.ErrStockUnavailable, err)
	}
	for _, table := range []string{"orders", "order_items", "order_status_logs"} {
		if n := f.tableCount(t, table); n != 0 {
			t.Fatalf("扣减失败必须整单回滚，%s 里应 0 行，实际 %d", table, n)
		}
	}
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("扣减失败不该动库存，实际 %d", got)
	}
}

// TestOrderCreateRollsBackWhenStockWriteFails 用一条**真实的**库存写入故障（不是替身）
// 再证一次建单与扣减同事务：库存流水的 INSERT 直接抛异常，而它发生在数量写回之后 ——
// 订单侧必须整单回滚，库存数量也必须退回去。
//
// 与上面替身那条的分工：替身证「订单侧怎么对待库存的错误」，这条证「跨模块句柄确实传进去了」
// （替身拿到的 tx 即便不是真事务，订单侧的回滚也会通过；真实故障才能证明是同一条事务）。
func TestOrderCreateRollsBackWhenStockWriteFails(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "真实故障商品", 30, 5)

	// 故障注入：库存流水的 INSERT 直接失败（与收货回滚用例同一手法，只在隔离 schema 里）。
	if err := f.db.Exec("CREATE OR REPLACE FUNCTION test_block_movement() RETURNS trigger AS $$ " +
		"BEGIN RAISE EXCEPTION '故障注入：库存流水写入失败'; END $$ LANGUAGE plpgsql").Error; err != nil {
		t.Fatalf("创建故障注入函数失败: %v", err)
	}
	if err := f.db.Exec("CREATE TRIGGER test_block_movement BEFORE INSERT ON inventory_stock_movements " +
		"FOR EACH ROW EXECUTE FUNCTION test_block_movement()").Error; err != nil {
		t.Fatalf("创建故障注入触发器失败: %v", err)
	}

	if _, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1)); err == nil {
		t.Fatalf("库存写入失败时建单必须整体失败")
	}
	for _, table := range []string{"orders", "order_items", "order_status_logs"} {
		if n := f.tableCount(t, table); n != 0 {
			t.Fatalf("库存写入失败必须整单回滚，%s 里应 0 行，实际 %d", table, n)
		}
	}
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("库存数量必须回滚（故障发生在写回之后），实际 %d", got)
	}
}

// TestOrderCancelStockFailureRollsBackWholeCancel 取消时归还库存失败 →
// 订单仍是 pending、没有取消流转、库存不动（不再有「已取消但库存没回来」的半截状态）。
func TestOrderCancelStockFailureRollsBackWholeCancel(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "取消回滚商品", 40, 5)
	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 3 {
		t.Fatalf("建单后库存应为 3，实际 %d", got)
	}

	// 同一批 model 换一个库存恒失败的 service 执行取消。
	broken := f.serviceWithStock(failingStockOperator{err: errors.New("stub: 库存服务不可用")})
	if _, cerr := broken.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "库存挂了",
	}); cerr == nil {
		t.Fatalf("归还库存失败时取消必须整体失败")
	}

	detail, derr := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if derr != nil {
		t.Fatalf("读订单详情失败: %v", derr)
	}
	if detail.Head.Status != ordermodel.OrderStatusPending {
		t.Fatalf("库存归还失败时订单状态应保持 pending，实际 %s", detail.Head.Status)
	}
	if len(detail.Logs) != 1 {
		t.Fatalf("流转链应只有建单那一条（不该出现取消流转），实际 %d 条", len(detail.Logs))
	}
	if got := f.stockOf(t, vid); got != 3 {
		t.Fatalf("取消回滚后库存应仍是 3，实际 %d", got)
	}
}

// TestReturnReceiveStockFailureKeepsApproved 退货入库失败 →
// 退货单留在 approved（门闩、库存、明细登记同事务），received_at 与明细都不落库。
func TestReturnReceiveStockFailureKeepsApproved(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "退货故障商品", 50, 5)
	orderID, userID, itemID := f.paidOrder(t, "return-stock-fail@example.com", vid, 1)
	if got := f.stockOf(t, vid); got != 4 {
		t.Fatalf("下单并付款后库存应为 4，实际 %d", got)
	}

	req := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 1}})
	req.UserID = userID
	created, err := f.orders.RequestReturn(ctx, req)
	if err != nil {
		t.Fatalf("提交退货申请失败: %v", err)
	}
	if _, err = f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{
		ReturnID: created.ID, Remark: "同意",
	}); err != nil {
		t.Fatalf("同意退货失败: %v", err)
	}

	// 入库阶段库存挂掉：状态推进与库存变动在同一事务里，必须一起回滚。
	broken := f.serviceWithStock(failingStockOperator{err: errors.New("stub: 库存服务不可用")})
	if _, rerr := broken.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{ReturnID: created.ID}); rerr == nil {
		t.Fatalf("入库失败时确认收货必须失败")
	}

	got, gerr := f.orders.GetReturn(ctx, created.ID)
	if gerr != nil {
		t.Fatalf("读退货单失败: %v", gerr)
	}
	if got.Return.Status != ordermodel.ReturnStatusApproved {
		t.Fatalf("入库失败后退货单应留在 approved，实际 %s", got.Return.Status)
	}
	if got.Return.ReceivedAt != nil {
		t.Fatalf("入库失败不该写 received_at，实际 %v", got.Return.ReceivedAt)
	}
	var received int
	if qerr := f.db.Raw("SELECT received_quantity FROM order_return_items WHERE return_id = ?", created.ID).
		Scan(&received).Error; qerr != nil {
		t.Fatalf("读退货明细失败: %v", qerr)
	}
	if received != 0 {
		t.Fatalf("入库失败不该登记已收数量，实际 %d", received)
	}
	if got := f.stockOf(t, vid); got != 4 {
		t.Fatalf("入库失败不该动库存（应仍是 4），实际 %d", got)
	}
}
