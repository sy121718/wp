package feature

import (
	"context"
	"errors"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"

	productdto "go_wp/internal/module/product/dto"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

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

func TestDeductAcrossWarehousesByPriority(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}

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

func TestSamePriorityDeductsByWarehouseCode(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	f.createWarehouse(t, "DF", "默认仓", true)

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

	if _, err := f.inventory.ReserveStock(context.Background(), &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-reserved",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 6, WarehouseID: a.ID}},
	}); err != nil {
		t.Fatalf("A 仓预占 6 件失败: %v", err)
	}

	if _, err := f.inventory.DeductStock(context.Background(), &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		SourceType: inventoryenums.MovementSourceOrderShipment, SourceRef: "SH-test",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, Quantity: 5,
		}},
	}); err != nil {
		t.Fatalf("出库 5 件失败: %v", err)
	}

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

func TestPurchaseTerminalStates(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "SUP1", "供应商一", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Purchase")
	v := f.firstVariant(t, p.ID)

	order, err := f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-TERM-1", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []purchasedto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 3, UnitPrice: 5,
		}},
	})
	if err != nil {
		t.Fatalf("建采购单失败: %v", err)
	}

	if _, err := f.purchase.FinishPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID,
	}); !errors.Is(err, purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotFinishable)) {
		t.Fatalf("pending 的单提前关单应报 ErrPurchaseStatusNotFinishable，实际 %v", err)
	}

	if _, err := f.purchase.RegisterReceipt(ctx, &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "req-term-1",
		Lines: []purchasedto.ReceiptLineReq{{LineID: order.Lines[0].ID, Quantity: 3}},
	}); err != nil {
		t.Fatalf("收货 3 件失败: %v", err)
	}
	finished, err := f.purchase.FinishPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID, Remark: "供应商断供",
	})
	if err != nil {
		t.Fatalf("received 的单提前关单应成功: %v", err)
	}
	if finished.Status != purchaseenums.PurchaseStatusFinished {
		t.Fatalf("关单后状态应是 finished，实际 %q", finished.Status)
	}

	var dbStatus string
	if qerr := f.db.Raw("SELECT status FROM inventory_purchase_orders WHERE id = ?", order.ID).Scan(&dbStatus).Error; qerr != nil {
		t.Fatalf("读状态失败: %v", qerr)
	}
	if dbStatus != purchaseenums.PurchaseStatusFinished {
		t.Fatalf("关单后库里的状态应是 finished，实际 %q", dbStatus)
	}

	if _, err := f.purchase.CancelPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID,
	}); !errors.Is(err, purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotCancelable)) {
		t.Fatalf("finished 的单取消应报 ErrPurchaseStatusNotCancelable，实际 %v", err)
	}

	before := f.movementCount(t)
	again, againErr := f.purchase.FinishPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID,
	})
	if againErr == nil {
		t.Fatalf("已 finished 的单再次关单应被拒（实际返回状态 %q）", again.Status)
	}
	if after := f.movementCount(t); after != before {
		t.Fatalf("关单不得写库存流水（之前 %d 条，之后 %d 条）", before, after)
	}
}

func TestPurchaseCancelPendingAndPartial(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "SUP2", "供应商二", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Cancelable")
	v := f.firstVariant(t, p.ID)

	order, err := f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-CANCEL-1", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []purchasedto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 4, UnitPrice: 3,
		}},
	})
	if err != nil {
		t.Fatalf("建采购单失败: %v", err)
	}

	cancelled, err := f.purchase.CancelPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order.ID, Remark: "改下别家",
	})
	if err != nil {
		t.Fatalf("pending 的单取消应成功: %v", err)
	}
	if cancelled.Status != purchaseenums.PurchaseStatusCancelled {
		t.Fatalf("取消后状态应是 cancelled，实际 %q", cancelled.Status)
	}

	order2, err := f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-CANCEL-2", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []purchasedto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 4, UnitPrice: 3,
		}},
	})
	if err != nil {
		t.Fatalf("建第二张采购单失败: %v", err)
	}
	if _, err := f.purchase.RegisterReceipt(ctx, &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order2.ID, RequestID: "req-cancel-2",
		Lines: []purchasedto.ReceiptLineReq{{LineID: order2.Lines[0].ID, Quantity: 1}},
	}); err != nil {
		t.Fatalf("收 1 件失败: %v", err)
	}
	partialCancel, err := f.purchase.CancelPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{
		ProjectID: f.projectID, ID: order2.ID,
	})
	if err != nil {
		t.Fatalf("partial 的单取消应成功: %v", err)
	}
	if partialCancel.Status != purchaseenums.PurchaseStatusCancelled {
		t.Fatalf("取消后状态应是 cancelled，实际 %q", partialCancel.Status)
	}

	if got := f.stockQty(t, v.ID, wh.ID); got != 1 {
		t.Fatalf("取消采购单不该动已入库的货（期望 1），实际 %d", got)
	}
}

func TestPurchaseStatusCheckConstraint(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "SUP3", "供应商三", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Check")
	v := f.firstVariant(t, p.ID)
	order, err := f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-CHECK-1", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []purchasedto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
			Quantity: 1, UnitPrice: 1,
		}},
	})
	if err != nil {
		t.Fatalf("建采购单失败: %v", err)
	}

	if err := f.db.Exec("INSERT INTO inventory_purchase_orders "+
		"(project_id, code, source_id, warehouse_id, status) VALUES (?, 'PO-BAD-STATUS', ?, ?, 'foo')",
		f.projectID, src.ID, wh.ID).Error; err == nil {
		t.Fatalf("非法状态值应被 CHECK 拒绝")
	}

	for _, status := range []string{"draft", "ordered", "partial", "received", "exception", "finished", "cancelled"} {
		if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = ? WHERE id = ?", status, order.ID).Error; err != nil {
			t.Fatalf("合法状态 %q 写入失败: %v", status, err)
		}
	}
}
