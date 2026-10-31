package feature

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
func (f failingStockOperator) ReserveStockTx(context.Context, *gorm.DB, *ordercontract.StockReservation) error {
	return f.err
}
func (f failingStockOperator) ReleaseStockTx(context.Context, *gorm.DB, *ordercontract.StockReservation) error {
	return f.err
}

var _ ordercontract.StockOperator = failingStockOperator{}

func TestOrderCreateStockFailureLeavesNothingBehind(t *testing.T) {
	f := newOrderFixtureWithStock(t, failingStockOperator{err: errors.New("stub: 库存服务不可用")})
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "库存故障商品", 30, 5)

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

func TestOrderCreateRollsBackWhenStockWriteFails(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "真实故障商品", 30, 5)

	if err := f.db.Exec("CREATE OR REPLACE FUNCTION test_block_reserve() RETURNS trigger AS $$ " +
		"BEGIN RAISE EXCEPTION '故障注入：预占写入失败'; END $$ LANGUAGE plpgsql").Error; err != nil {
		t.Fatalf("创建故障注入函数失败: %v", err)
	}
	if err := f.db.Exec("CREATE TRIGGER test_block_reserve BEFORE UPDATE ON inventory_stocks " +
		"FOR EACH ROW EXECUTE FUNCTION test_block_reserve()").Error; err != nil {
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
	if got := f.reservedOf(t, vid); got != 0 {
		t.Fatalf("预占必须回滚，实际 %d", got)
	}
}

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

	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("建单后预占应为 2，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("建单后库存应为 5（只预占不扣减），实际 %d", got)
	}

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
	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("取消回滚后预占应仍是 2，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("取消回滚后库存应仍是 5，实际 %d", got)
	}
}

func TestReturnReceiveStockFailureKeepsApproved(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "退货故障商品", 50, 5)
	orderID, userID, itemID := f.paidOrder(t, "return-stock-fail@example.com", vid, 1)

	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("下单并付款后在库应为 5（只预占），实际 %d", got)
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
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("入库失败不该动库存（应仍是 5），实际 %d", got)
	}
}
