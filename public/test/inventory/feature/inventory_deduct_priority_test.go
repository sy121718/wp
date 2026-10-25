// inventory_deduct_priority_test.go — W2（迁移 623）的验收：跨仓扣减优先级 + 采购单终态。
//
// 覆盖任务书 §5.6 的会红断言：
//
//	1  仓库 A（priority 10）8 件、B（priority 20）8 件 → 出库 12 件不指定仓：A 减 8、B 减 4
//	2  显式指定 B 出库 5 件 → A 一字不变，B 减 5（优先级不得覆盖显式选择）
//	3  总可售不足（A 2 + B 3，要 6）→ ErrStockInsufficient、两仓 quantity 均未变
//	4  同 priority 的两仓按 code 升序扣减
//	5  预占行参与：A 有 8 件其中 reserved 6（可售 2）、B 有 8 → 出库 5 → A 减 2、B 减 3
//	6  采购：received → finished 成功；finished → cancelled 返回 ErrPurchaseStatusNotCancelable
//	7  迁移后 CHECK 生效：UPDATE ... SET status='foo' 报约束错
package feature

import (
	"context"
	"errors"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"

	productdto "go_wp/internal/module/product/dto"
)

// createWarehouseWithPriority 建仓并指定扣减优先级（迁移 623）。
func (f *invFixture) createWarehouseWithPriority(t *testing.T, code, name string, priority int) *inventorydto.WarehouseResp {
	t.Helper()
	w, err := f.inventory.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: code, Name: name, DeductPriority: priority,
	})
	if err != nil {
		t.Fatalf("建仓 %s（优先级 %d）失败: %v", code, priority, err)
	}
	if w.DeductPriority != priority {
		t.Fatalf("建仓后优先级应是 %d，实际 %d", priority, w.DeductPriority)
	}
	return w
}

// deductAcross 出库（不指定仓，触发跨仓分配）。
func deductAcross(t *testing.T, f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp, quantity int) error {
	t.Helper()
	_, err := f.inventory.DeductStock(context.Background(), &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		SourceType: "order", SourceRef: "SO-test",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, Quantity: quantity,
		}},
	})
	return err
}

// TestDeductAcrossWarehousesByPriority 验收 1：按优先级跨仓分配。
func TestDeductAcrossWarehousesByPriority(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	// 默认仓单独建（避开默认仓兜底，让「不指定仓」真的走跨仓分配）。
	f.createWarehouse(t, "DF", "默认仓", true)
	a := f.createWarehouseWithPriority(t, "AA", "A 仓", 10)
	b := f.createWarehouseWithPriority(t, "BB", "B 仓", 20)
	p := mustProduct(t, f, "Across")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, a.ID, 8, "purchase_in")
	changeIn(t, f, p, v, b.ID, 8, "purchase_in")

	if err := deductAcross(t, f, p, v, 12); err != nil {
		t.Fatalf("跨仓出库 12 件失败: %v", err)
	}
	if got := f.stockQty(t, v.ID, a.ID); got != 0 {
		t.Fatalf("优先级 10 的仓应先扣满 8 件（期望 0），实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, b.ID); got != 4 {
		t.Fatalf("剩余 4 件应扣在优先级 20 的仓上（期望 4），实际 %d", got)
	}
}

// TestExplicitWarehouseBeatsPriority 验收 2：显式指定的仓不被优先级覆盖。
func TestExplicitWarehouseBeatsPriority(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	f.createWarehouse(t, "DF", "默认仓", true)
	a := f.createWarehouseWithPriority(t, "AA", "A 仓", 10)
	b := f.createWarehouseWithPriority(t, "BB", "B 仓", 20)
	p := mustProduct(t, f, "Explicit")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, a.ID, 8, "purchase_in")
	changeIn(t, f, p, v, b.ID, 8, "purchase_in")

	if _, err := f.inventory.DeductStock(context.Background(), &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		SourceType: "order", SourceRef: "SO-explicit",
		WarehouseID: b.ID,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, Quantity: 5,
		}},
	}); err != nil {
		t.Fatalf("显式指定 B 仓出库 5 件失败: %v", err)
	}
	if got := f.stockQty(t, v.ID, a.ID); got != 8 {
		t.Fatalf("显式指定 B 仓时 A 仓一字不能动（期望 8），实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, b.ID); got != 3 {
		t.Fatalf("B 仓应减 5（期望 3），实际 %d", got)
	}
}

// TestCrossWarehouseInsufficientRollsBack 验收 3：总可售不足 → 整单拒绝，两仓都不动。
func TestCrossWarehouseInsufficientRollsBack(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	f.createWarehouse(t, "DF", "默认仓", true)
	a := f.createWarehouseWithPriority(t, "AA", "A 仓", 10)
	b := f.createWarehouseWithPriority(t, "BB", "B 仓", 20)
	p := mustProduct(t, f, "Insufficient")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, a.ID, 2, "purchase_in")
	changeIn(t, f, p, v, b.ID, 3, "purchase_in")

	if err := deductAcross(t, f, p, v, 6); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockInsufficient)) {
		t.Fatalf("总可售 5 件要出 6 件应报 ErrStockInsufficient，实际 %v", err)
	}
	if got := f.stockQty(t, v.ID, a.ID); got != 2 {
		t.Fatalf("整单拒绝时 A 仓不该变（期望 2），实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, b.ID); got != 3 {
		t.Fatalf("整单拒绝时 B 仓不该变（期望 3），实际 %d", got)
	}
}

// TestSamePriorityDeductsByWarehouseCode 验收 4：同优先级按仓短码升序。
func TestSamePriorityDeductsByWarehouseCode(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	f.createWarehouse(t, "DF", "默认仓", true)
	// 同优先级 10；短码 AA 排在 ZZ 之前。
	aa := f.createWarehouseWithPriority(t, "AA", "A 仓", 10)
	zz := f.createWarehouseWithPriority(t, "ZZ", "Z 仓", 10)
	p := mustProduct(t, f, "SamePriority")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, aa.ID, 4, "purchase_in")
	changeIn(t, f, p, v, zz.ID, 4, "purchase_in")

	if err := deductAcross(t, f, p, v, 3); err != nil {
		t.Fatalf("同优先级出库 3 件失败: %v", err)
	}
	if got := f.stockQty(t, v.ID, aa.ID); got != 1 {
		t.Fatalf("同优先级应扣在短码小的仓（AA 期望 1），实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, zz.ID); got != 4 {
		t.Fatalf("短码大的仓不该被扣（ZZ 期望 4），实际 %d", got)
	}
}

// TestCrossWarehouseUsesAvailableNotOnHand 验收 5：分配用可售（扣掉预占），不是 quantity。
func TestCrossWarehouseUsesAvailableNotOnHand(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	f.createWarehouse(t, "DF", "默认仓", true)
	a := f.createWarehouseWithPriority(t, "AA", "A 仓", 10)
	b := f.createWarehouseWithPriority(t, "BB", "B 仓", 20)
	p := mustProduct(t, f, "Reserved")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, a.ID, 8, "purchase_in")
	changeIn(t, f, p, v, b.ID, 8, "purchase_in")
	// A 仓预占 6（可售只剩 2）。
	if _, err := f.inventory.ReserveStock(context.Background(), &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-reserved",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 6, WarehouseID: a.ID}},
	}); err != nil {
		t.Fatalf("A 仓预占 6 件失败: %v", err)
	}

	// 发货出库（source_type = order_shipment）：只有这条路径才同时释放预占 ——
	// 与 S2 改造后的订单链路一致（建单预占 / 发货扣减）。
	if _, err := f.inventory.DeductStock(context.Background(), &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		SourceType: inventoryenums.MovementSourceOrderShipment, SourceRef: "SH-test",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, Quantity: 5,
		}},
	}); err != nil {
		t.Fatalf("出库 5 件失败: %v", err)
	}
	// A 的可售只有 2，扣完剩 6 件在库（预占 6 保留）；B 扣 3。
	if got := f.stockQty(t, v.ID, a.ID); got != 6 {
		t.Fatalf("A 仓只能按可售扣 2 件（8→6），实际 %d", got)
	}
	if got := f.stockReserved(t, v.ID, a.ID); got != 4 {
		t.Fatalf("A 仓预占应被释放 2 件（6→4），实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, b.ID); got != 5 {
		t.Fatalf("剩余 3 件应扣在 B 仓（8→5），实际 %d", got)
	}
}

// TestPurchaseTerminalStates 验收 6：received → finished 成功；finished → cancelled 打回。
func TestPurchaseTerminalStates(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "SUP1", "供应商一", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Purchase")
	v := f.firstVariant(t, p.ID)

	order, err := f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-TERM-1", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []inventorydto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 3, UnitPrice: 5,
		}},
	})
	if err != nil {
		t.Fatalf("建采购单失败: %v", err)
	}
	// pending 的单不能提前关单（还没收到货，关单没有意义）。
	if _, err := f.inventory.FinishPurchaseOrder(ctx, &inventorydto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrPurchaseStatusNotFinishable)) {
		t.Fatalf("pending 的单提前关单应报 ErrPurchaseStatusNotFinishable，实际 %v", err)
	}
	// 收满 → received。
	if _, err := f.inventory.RegisterReceipt(ctx, &inventorydto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "req-term-1",
		Lines: []inventorydto.ReceiptLineReq{{LineID: order.Lines[0].ID, Quantity: 3}},
	}); err != nil {
		t.Fatalf("收货 3 件失败: %v", err)
	}
	finished, err := f.inventory.FinishPurchaseOrder(ctx, &inventorydto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID, Remark: "供应商断供",
	})
	if err != nil {
		t.Fatalf("received 的单提前关单应成功: %v", err)
	}
	if finished.Status != inventoryenums.PurchaseStatusFinished {
		t.Fatalf("关单后状态应是 finished，实际 %q", finished.Status)
	}
	// 库里也必须是 finished：回执取自锁内的行，单看回执会漏掉「守卫把它改回去了」这类错。
	var dbStatus string
	if qerr := f.db.Raw("SELECT status FROM inventory_purchase_orders WHERE id = ?", order.ID).Scan(&dbStatus).Error; qerr != nil {
		t.Fatalf("读状态失败: %v", qerr)
	}
	if dbStatus != inventoryenums.PurchaseStatusFinished {
		t.Fatalf("关单后库里的状态应是 finished，实际 %q", dbStatus)
	}
	// finished 不能再取消（终态单向）。
	if _, err := f.inventory.CancelPurchaseOrder(ctx, &inventorydto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrPurchaseStatusNotCancelable)) {
		t.Fatalf("finished 的单取消应报 ErrPurchaseStatusNotCancelable，实际 %v", err)
	}
	// 关单不写库存流水：收货那一次流水仍在，关单之后条数不变。
	before := f.movementCount(t)
	again, againErr := f.inventory.FinishPurchaseOrder(ctx, &inventorydto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID,
	})
	if againErr == nil {
		t.Fatalf("已 finished 的单再次关单应被拒（实际返回状态 %q）", again.Status)
	}
	if after := f.movementCount(t); after != before {
		t.Fatalf("关单不得写库存流水（之前 %d 条，之后 %d 条）", before, after)
	}
}

// TestPurchaseCancelPendingAndPartial 取消的两条合法路径：pending / partial。
func TestPurchaseCancelPendingAndPartial(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "SUP2", "供应商二", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Cancelable")
	v := f.firstVariant(t, p.ID)

	order, err := f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-CANCEL-1", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []inventorydto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 4, UnitPrice: 3,
		}},
	})
	if err != nil {
		t.Fatalf("建采购单失败: %v", err)
	}
	// pending 可以取消。
	cancelled, err := f.inventory.CancelPurchaseOrder(ctx, &inventorydto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID, Remark: "改下别家",
	})
	if err != nil {
		t.Fatalf("pending 的单取消应成功: %v", err)
	}
	if cancelled.Status != inventoryenums.PurchaseStatusCancelled {
		t.Fatalf("取消后状态应是 cancelled，实际 %q", cancelled.Status)
	}
	// 部分入库的单也可以取消。
	order2, err := f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-CANCEL-2", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []inventorydto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 4, UnitPrice: 3,
		}},
	})
	if err != nil {
		t.Fatalf("建第二张采购单失败: %v", err)
	}
	if _, err := f.inventory.RegisterReceipt(ctx, &inventorydto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order2.ID, RequestID: "req-cancel-2",
		Lines: []inventorydto.ReceiptLineReq{{LineID: order2.Lines[0].ID, Quantity: 1}},
	}); err != nil {
		t.Fatalf("收 1 件失败: %v", err)
	}
	partialCancel, err := f.inventory.CancelPurchaseOrder(ctx, &inventorydto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order2.ID,
	})
	if err != nil {
		t.Fatalf("partial 的单取消应成功: %v", err)
	}
	if partialCancel.Status != inventoryenums.PurchaseStatusCancelled {
		t.Fatalf("取消后状态应是 cancelled，实际 %q", partialCancel.Status)
	}
	// 已入库的 1 件仍在仓里（取消不动库存）。
	if got := f.stockQty(t, v.ID, wh.ID); got != 1 {
		t.Fatalf("取消采购单不该动已入库的货（期望 1），实际 %d", got)
	}
}

// TestPurchaseStatusCheckConstraint 验收 7：迁移后 CHECK 认五态、拒非法值。
func TestPurchaseStatusCheckConstraint(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "SUP3", "供应商三", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Check")
	v := f.firstVariant(t, p.ID)
	order, err := f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-CHECK-1", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []inventorydto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 1, UnitPrice: 1,
		}},
	})
	if err != nil {
		t.Fatalf("建采购单失败: %v", err)
	}
	// 单头守卫（迁移 196 改版后）会把非终态改写成推导值，所以非法值不能经 UPDATE 测 ——
	// 它会先被守卫换成 'pending'，CHECK 根本没机会说话。改用 INSERT 直接验约束本身。
	if err := f.db.Exec("INSERT INTO inventory_purchase_orders "+
		"(project_id, code, source_id, warehouse_id, status) VALUES (?, 'PO-BAD-STATUS', ?, ?, 'foo')",
		f.projectID, src.ID, wh.ID).Error; err == nil {
		t.Fatalf("非法状态值应被 CHECK 拒绝")
	}
	// 五个合法值都能写（约束认全）。
	for _, status := range []string{"pending", "partial", "received", "finished", "cancelled"} {
		if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = ? WHERE id = ?", status, order.ID).Error; err != nil {
			t.Fatalf("合法状态 %q 写入失败: %v", status, err)
		}
	}
}
