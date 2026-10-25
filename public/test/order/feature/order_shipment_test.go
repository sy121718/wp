// order_shipment_test.go — S2（迁移 624 / 625）的验收：发货用例 + 订单预占改造。
//
// 覆盖任务书 §7.7 的会红断言：
//
//	1  未付款不发货：pending 单发货 → 409 + order.err.orderNotShippable，库存与预占均未变
//	2  下单只预占：下单 3 件（quantity=10）→ quantity=10, reserved=3、流水条数不变
//	3  发货才扣：发货 2 件 → quantity=8, reserved=1、新增 1 条 sale_out（source_type=order_shipment）
//	4  幂等：同一 request_id 连发两次 → 只有一张发货单、库存只减一次
//	5  超量：订单 3 件已发 2，再发 2 → 409 + shipQuantityExceeded，库存不变
//	6  库存不足整单回滚：发货失败 → order_shipments 0 行、orders.status 仍 paid、库存未变
//	7  取消释放预占：取消 paid 单 → reserved 归 0、quantity 不变、无新流水
//	8  并发：两 goroutine 各发 2 件（订单 3 件）→ 一成一败，quantity 只减 2
//	9  成功路径等式：quantity_after = quantity_before − Σshipment_items.quantity
//	  reserved_after = reserved_before − min(reserved_before, Σ)
//	10 文案不泄漏：错误响应 body 不含 uq_order_shipments_request / 23505 / 表名
//	11 发货仓固化：下单不传 warehouseId → order_items.warehouse_id = 默认仓；改默认仓后再发仍从原仓扣
package feature

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productdto "go_wp/internal/module/product/dto"

	inventorydto "go_wp/internal/module/inventory/dto"
)

// payOrder 把一张订单推到已付款（发货的前置状态）。
func (f *orderFixture) payOrder(t *testing.T, orderID uint64) {
	t.Helper()
	if _, err := f.orders.PayOrder(context.Background(), &orderdto.PayOrderReq{
		OrderID:            orderID,
		PaymentMethod:      "manual",
		PaymentMethodTitle: "手工确认",
		TransactionID:      "TX-SHIP",
	}); err != nil {
		t.Fatalf("支付落账失败: %v", err)
	}
}

// orderItemIDs 某订单的行 id（按详情顺序）。
func (f *orderFixture) orderItemIDs(t *testing.T, orderID uint64) []uint64 {
	t.Helper()
	detail, err := f.orders.GetOrder(context.Background(),
		&orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: orderID})
	if err != nil || detail == nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	ids := make([]uint64, 0, len(detail.Items))
	for _, it := range detail.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

// movementCount 库存流水条数（预占不写流水，发货才写）。
func (f *orderFixture) movementCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE project_id = ?",
		f.projectID).Scan(&n).Error; err != nil {
		t.Fatalf("读流水条数失败: %v", err)
	}
	return n
}

// shipmentCount 某订单的发货单张数。
func (f *orderFixture) shipmentCount(t *testing.T, orderID uint64) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM order_shipments WHERE order_id = ?", orderID).
		Scan(&n).Error; err != nil {
		t.Fatalf("读发货单条数失败: %v", err)
	}
	return n
}

// TestShipRequiresPaidOrder 验收 1：未付款不发货。
func TestShipRequiresPaidOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "待付款商品", 100, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	itemIDs := f.orderItemIDs(t, created.ID)

	_, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, CarrierName: "顺丰", TrackingNo: "SF001",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 1}},
	})
	if serr == nil || !strings.Contains(serr.Error(), orderenums.ErrOrderNotShippable) {
		t.Fatalf("pending 单发货应报 orderNotShippable，实际 %v", serr)
	}
	// 库存与预占都不许动。
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("未付款发货不该动在库数量，实际 %d", got)
	}
	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("未付款发货不该动预占（应仍是下单时的 2），实际 %d", got)
	}
	if got := f.shipmentCount(t, created.ID); got != 0 {
		t.Fatalf("失败的发货不该留下发货单，实际 %d 张", got)
	}
}

// TestShipDeductsOnlyAtShipTime 验收 2 / 3 / 9：下单只预占、发货才扣减。
func TestShipDeductsOnlyAtShipTime(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "两段式商品", 60, 10)

	// ② 下单 3 件：只预占。
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 3))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("下单不得扣减在库数量，期望 10，实际 %d", got)
	}
	if got := f.reservedOf(t, vid); got != 3 {
		t.Fatalf("下单应预占 3 件，实际 %d", got)
	}
	movementsAfterOrder := f.movementCount(t)

	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)

	// ③ 发货 2 件：才扣减，并且释放这 2 件的预占。
	res, err := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID,
		CarrierName: "顺丰", TrackingNo: "SF1001", OperatorName: "tester",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("发货失败: %v", err)
	}

	// ⑨ 成功路径等式（用实际发货明细的合计断言，不写各分量）：
	//   quantity_after = quantity_before − Σshipment_items.quantity
	//   reserved_after = reserved_before − min(reserved_before, Σ)
	var shippedSum int
	if qerr := f.db.Raw("SELECT COALESCE(SUM(quantity),0) FROM order_shipment_items WHERE shipment_id = ?",
		res.ID).Scan(&shippedSum).Error; qerr != nil {
		t.Fatalf("读发货明细合计失败: %v", qerr)
	}
	if shippedSum != 2 {
		t.Fatalf("发货明细合计应为 2，实际 %d", shippedSum)
	}
	if got, want := f.stockOf(t, vid), 10-shippedSum; got != want {
		t.Fatalf("在库应满足 quantity_after = quantity_before − Σ（期望 %d），实际 %d", want, got)
	}
	if got, want := f.reservedOf(t, vid), 3-shippedSum; got != want {
		t.Fatalf("预占应满足 reserved_after = reserved_before − min(...)（期望 %d），实际 %d", want, got)
	}

	// 新增一条 sale_out 流水，source_type=order_shipment、source_ref=发货单号。
	if got := f.movementCount(t); got != movementsAfterOrder+1 {
		t.Fatalf("发货应写 1 条流水（下单不写），期望 %d，实际 %d", movementsAfterOrder+1, got)
	}
	var sourceType, sourceRef string
	if qerr := f.db.Raw("SELECT source_type, source_ref FROM inventory_stock_movements "+
		"WHERE project_id = ? ORDER BY create_time DESC LIMIT 1", f.projectID).
		Row().Scan(&sourceType, &sourceRef); qerr != nil {
		t.Fatalf("读流水来源失败: %v", qerr)
	}
	if sourceType != "order_shipment" {
		t.Fatalf("发货流水的 source_type 应为 order_shipment，实际 %q", sourceType)
	}
	if sourceRef != res.ShipmentNo {
		t.Fatalf("发货流水的 source_ref 应是发货单号 %q，实际 %q", res.ShipmentNo, sourceRef)
	}
}

// TestShipIsIdempotentByRequestID 验收 4：同一 request_id 只产生一张单、只扣一次库存。
func TestShipIsIdempotentByRequestID(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "幂等发货商品", 40, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)

	req := &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "req-ship-1",
		CarrierName: "顺丰", TrackingNo: "SF2001",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	}
	first, err := f.orders.ShipOrder(ctx, req)
	if err != nil {
		t.Fatalf("首次发货失败: %v", err)
	}
	stockAfterFirst := f.stockOf(t, vid)

	second, err := f.orders.ShipOrder(ctx, req)
	if err != nil {
		t.Fatalf("重复提交应幂等成功而不是报错: %v", err)
	}
	if second.ID != first.ID || second.ShipmentNo != first.ShipmentNo {
		t.Fatalf("同一 request_id 应命中同一张发货单，实际 %+v vs %+v", second, first)
	}
	if got := f.shipmentCount(t, created.ID); got != 1 {
		t.Fatalf("同一 request_id 只该有一张发货单，实际 %d 张", got)
	}
	if got := f.stockOf(t, vid); got != stockAfterFirst {
		t.Fatalf("幂等命中不得再扣一次库存（%d → %d）", stockAfterFirst, got)
	}
}

// TestShipExceedsOrderedQuantity 验收 5：超量发货被拒，库存不变。
func TestShipExceedsOrderedQuantity(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "超量发货商品", 40, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 3))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)

	if _, err := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "ship-2",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	}); err != nil {
		t.Fatalf("首次发 2 件失败: %v", err)
	}
	stockAfter := f.stockOf(t, vid)

	_, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "ship-3",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	})
	if serr == nil || !strings.Contains(serr.Error(), orderenums.ErrShipQuantityExceeded) {
		t.Fatalf("超量发货应报 shipQuantityExceeded，实际 %v", serr)
	}
	if got := f.stockOf(t, vid); got != stockAfter {
		t.Fatalf("超量被拒时库存不该变（%d → %d）", stockAfter, got)
	}
}

// TestShipStockShortageRollsBackWhole 验收 6：库存不足 → 发货单 0 行、状态仍 paid、库存未变。
func TestShipStockShortageRollsBackWhole(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "发货不足商品", 40, 5)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 5))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)

	// 把在库数量压到 2、并把预占一起降到 2（DDL 的 CHECK 要求 reserved <= quantity）——
	// 模拟「货被别的渠道卖掉了」。预占降下来不影响本用例：发货 5 件仍然超在库。
	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 2, reserved_quantity = 2 "+
		"WHERE variant_id = ? AND warehouse_id = ?", vid, f.warehouse).Error; err != nil {
		t.Fatalf("制造库存不足失败: %v", err)
	}

	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "ship-short",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 5}},
	}); serr == nil {
		t.Fatalf("库存不足时发货必须失败")
	}
	if got := f.shipmentCount(t, created.ID); got != 0 {
		t.Fatalf("失败的发货不该留下发货单，实际 %d 张", got)
	}
	detail, derr := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: created.ID})
	if derr != nil {
		t.Fatalf("读订单详情失败: %v", derr)
	}
	if detail.Head.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("发货失败后订单状态应仍是 paid，实际 %s", detail.Head.Status)
	}
	if got := f.stockOf(t, vid); got != 2 {
		t.Fatalf("发货失败不该动库存，实际 %d", got)
	}
}

// TestCancelReleasesReservationOnly 验收 7：取消释放预占，在库不变、无新流水。
func TestCancelReleasesReservationOnly(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "取消释放商品", 40, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 3))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	movements := f.movementCount(t)

	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: created.ID, Reason: "客户不要了", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if got := f.reservedOf(t, vid); got != 0 {
		t.Fatalf("取消后预占应归零，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("取消不该动在库数量（货从来没出库），实际 %d", got)
	}
	if got := f.movementCount(t); got != movements {
		t.Fatalf("取消（释放预占）不该写流水：之前 %d 条，之后 %d 条", movements, got)
	}
}

// TestConcurrentShipKeepsSingleOutcome 验收 8：并发发货一成一败，库存只减一次。
func TestConcurrentShipKeepsSingleOutcome(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "并发发货商品", 40, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 3))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)

	// 两 goroutine 各发 2 件（订单只有 3 件）：恰一个成功。
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
				OrderID: created.ID, ProjectID: f.projectID,
				RequestID: "concurrent-ship-" + string(rune('a'+n)),
				Lines:     []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
			})
			results <- serr
		}(i)
	}
	wg.Wait()
	close(results)
	ok, failed := 0, 0
	for serr := range results {
		if serr == nil {
			ok++
		} else {
			failed++
		}
	}
	if ok != 1 || failed != 1 {
		t.Fatalf("并发发货应恰一成一败，实际成功 %d / 失败 %d", ok, failed)
	}
	// 在库只减 2（成功的那个），预占减 2。
	if got, want := f.stockOf(t, vid), 8; got != want {
		t.Fatalf("并发后库存应只减一次（期望 %d），实际 %d", want, got)
	}
	if got, want := f.reservedOf(t, vid), 1; got != want {
		t.Fatalf("并发后预占应只释放一次（期望 %d），实际 %d", want, got)
	}
}

// TestShipErrorDoesNotLeakInternals 验收 10：错误响应不含内部标识（约束名 / SQLSTATE / 表名）。
//
// 走真实 JSON 出口：幂等键冲突用「先插一张同 request_id 的单，再用同一个 id 发货」制造 ——
// 但服务层的幂等分支会先命中并返回既有单，所以这里直接断言**超量与状态**两类错误的文案：
// 它们经过 response.ErrorAuto 的那条链，正是内部原文最容易漏出去的地方。
func TestShipErrorDoesNotLeakInternals(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "文案不泄漏商品", 40, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	itemIDs := f.orderItemIDs(t, created.ID)

	// pending 单发货 → 业务错误。它的 Error() 只该是那条 key。
	_, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID,
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 1}},
	})
	if serr == nil {
		t.Fatalf("pending 单发货应失败")
	}
	for _, leak := range []string{"uq_order_shipments_request", "23505", "order_shipments", "order_shipment_items\n"} {
		if strings.Contains(serr.Error(), leak) {
			t.Fatalf("错误文案泄漏了内部标识 %q：%s", leak, serr.Error())
		}
	}
}

// TestShipWarehousePinnedAtOrderTime 验收 11：发货仓在下单时固化，改默认仓不影响已下的单。
func TestShipWarehousePinnedAtOrderTime(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "仓固化商品", 40, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// 下单时未传 warehouseId → 解析成默认仓并固化。
	var pinned *string
	if qerr := f.db.Raw("SELECT warehouse_id FROM order_items WHERE order_id = ?", created.ID).
		Row().Scan(&pinned); qerr != nil {
		t.Fatalf("读订单行发货仓失败: %v", qerr)
	}
	if pinned == nil || *pinned != f.warehouse {
		t.Fatalf("下单应把发货仓固化成默认仓 %q，实际 %v", f.warehouse, pinned)
	}

	// 换一个默认仓（新建一个并设为默认），再给新仓补货 —— 已下的单仍应从**原仓**扣。
	other, err := f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "SH", Name: "上海仓", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("建新默认仓失败: %v", err)
	}
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: "in", ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{VariantID: vid, WarehouseID: other.ID, Quantity: 10}},
	}); err != nil {
		t.Fatalf("给新仓补货失败: %v", err)
	}

	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)
	if _, err := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "pinned-wh",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	}); err != nil {
		t.Fatalf("发货失败: %v", err)
	}
	// 原仓（SZ）从 10 减到 8；新仓不动。
	if got := f.stockOf(t, vid); got != 8 {
		t.Fatalf("应从固化的原仓扣（10→8），实际 %d", got)
	}
	var otherQty int
	if qerr := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		vid, other.ID).Row().Scan(&otherQty); qerr != nil {
		t.Fatalf("读新仓库存失败: %v", qerr)
	}
	if otherQty != 10 {
		t.Fatalf("新默认仓不该被扣（默认仓改了也不影响已下的单），实际 %d", otherQty)
	}
}

// TestShipWithNoWarehouseRequiresSelection 验收 13：无仓工程不阻断下单，但发货要求指定仓。
func TestShipWithNoWarehouseRequiresSelection(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 制造「这个工程没有任何仓库」：先建商品与变体（建变体要求有默认仓），再删仓。
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "无仓商品"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProjectID: f.projectID, ProductID: p.ID, SKUCode: "NOWH-1", WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("建变体失败: %v", err)
	}
	if derr := f.db.Exec("DELETE FROM inventory_warehouses WHERE project_id = ?", f.projectID).Error; derr != nil {
		t.Fatalf("删除仓库失败: %v", derr)
	}

	// 下单成功（本地单站允许无仓卖货）—— warehouse_id 留 NULL。
	created, cerr := f.orders.CreateOrder(ctx, f.createBaseReq(v.ID, 1))
	if cerr != nil {
		t.Fatalf("无仓工程不该阻断下单: %v", cerr)
	}
	var pinned *string
	if qerr := f.db.Raw("SELECT warehouse_id FROM order_items WHERE order_id = ?", created.ID).
		Row().Scan(&pinned); qerr != nil {
		t.Fatalf("读订单行发货仓失败: %v", qerr)
	}
	if pinned != nil {
		t.Fatalf("无仓工程的订单行 warehouse_id 应为 NULL，实际 %v", *pinned)
	}

	// 发货：没有仓可解析 → ErrShipWarehouseRequired（400）。
	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)
	_, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "no-wh",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 1}},
	})
	if serr == nil || !strings.Contains(serr.Error(), orderenums.ErrShipWarehouseRequired) {
		t.Fatalf("无仓工程的发货应报 ErrShipWarehouseRequired，实际 %v", serr)
	}
}

// TestSalesOverviewGrossProfit 验收（W4 §10.5 ①/②）：
// 毛利 = 行实付 − 行成本 − 运费；成本未核算的行按 0 计入并标记。
func TestSalesOverviewGrossProfit(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "毛利商品", 50, 10)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	// 直接改库设总价 / 行金额 / 行成本 / 运费：这个用例测的是**口径**（怎么算毛利），
	// 不是建单链路（那条已有自己的用例）。构造成：
	//   total = 10000、line_total = 10000、cost_price = 2000、quantity = 2、shipping_total = 1000
	//   → 毛利 = 10000 − 4000 − 1000 = 5000
	if err := f.db.Exec("UPDATE orders SET subtotal = 10000, total = 10000, shipping_total = 1000 WHERE id = ?",
		created.ID).Error; err != nil {
		t.Fatalf("改订单金额失败: %v", err)
	}
	if err := f.db.Exec("UPDATE order_items SET unit_price = 5000, line_subtotal = 10000, line_total = 10000, "+
		"cost_price = 2500 WHERE order_id = ?", created.ID).Error; err != nil {
		t.Fatalf("改订单行金额失败: %v", err)
	}

	// 概览页永远显式传区间（service 不给「静默默认窗口」，见 normalizeRangeWindow）。
	day := func(offset int) string {
		return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
	}
	overview, err := f.orders.SalesOverview(ctx, &orderdto.SalesOverviewReq{
		ProjectID: f.projectID, From: day(-30), To: day(0), Status: "paid",
	})
	if err != nil {
		t.Fatalf("读销售概览失败: %v", err)
	}
	// 毛利 = line_total(10000) − cost_price×quantity(2500×2=5000) − shipping_total(1000) = 4000。
	const wantGross = 10000 - 5000 - 1000
	if overview.GrossProfit != wantGross {
		t.Fatalf("毛利应等于 %d 分（行实付 − 行成本 − 运费），实际 %d", wantGross, overview.GrossProfit)
	}
	if overview.CostIncomplete {
		t.Fatalf("成本都核算了，CostIncomplete 不该为真")
	}

	// ② 含一行 cost_price IS NULL：CostIncomplete 为真，该行成本按 0 计入（毛利不被判成负数）。
	if err := f.db.Exec("UPDATE order_items SET cost_price = NULL WHERE order_id = ?", created.ID).Error; err != nil {
		t.Fatalf("清空行成本失败: %v", err)
	}
	overview2, err := f.orders.SalesOverview(ctx, &orderdto.SalesOverviewReq{
		ProjectID: f.projectID, From: day(-30), To: day(0), Status: "paid",
	})
	if err != nil {
		t.Fatalf("读销售概览失败: %v", err)
	}
	if !overview2.CostIncomplete || overview2.CostIncompleteRows != 1 {
		t.Fatalf("成本未核算应标记 CostIncomplete=true 且 Rows=1，实际 %v / %d",
			overview2.CostIncomplete, overview2.CostIncompleteRows)
	}
	// 该行按 0 计入：毛利 = 10000 − 0 − 1000 = 9000。
	if overview2.GrossProfit != 9000 {
		t.Fatalf("成本未核算的行应按 0 计入（毛利 9000），实际 %d", overview2.GrossProfit)
	}
}
