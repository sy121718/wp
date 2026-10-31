package feature

import (
	"context"
	"testing"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	orderstock "go_wp/internal/module/inventory/outbound/orderstock"
	ordercontract "go_wp/internal/module/order/contract"
	productdto "go_wp/internal/module/product/dto"
)

func restockViaOrderTx(t *testing.T, f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp,
	warehouseID string, quantity int, sourceType string) error {
	t.Helper()
	op := orderstock.New(f.inventory)
	return f.db.Transaction(func(tx *gorm.DB) error {
		return op.ChangeStockTx(context.Background(), tx, &ordercontract.StockAdjustment{
			ProjectID:  f.projectID,
			ReasonCode: "return_in",
			SourceType: sourceType,
			SourceRef:  "SO-RESTOCK-1",
			Remark:     "取消订单归还库存：测试",
			Lines: []ordercontract.StockLine{{
				ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
				WarehouseID: warehouseID, Quantity: quantity,
			}},
		})
	})
}

func TestRestockSkipsUntrackedRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "无限商品归还")
	v := f.firstVariant(t, p.ID)

	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("前置条件不成立：新建行应默认不跟踪")
	}

	if err := changeOut(f, p, v, wh.ID, 5, "SO-RESTOCK-OUT"); err != nil {
		t.Fatalf("无限行出库不该被拒：%v", err)
	}
	if err := restockViaOrderTx(t, f, p, v, wh.ID, 5, "order"); err != nil {
		t.Fatalf("取消订单归还失败：%v", err)
	}

	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("归还把无限行切成了跟踪 —— 商品会从「无限」变成「库存 5」，第 6 件被判库存不足")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("归还给无限行写入了数量 %d（CHECK 允许不跟踪行只能为 0，说明开关也被切了）", got)
	}
	if n := countMovements(t, f, v.ID); n != 0 {
		t.Fatalf("归还对无限行没有数量变动，不该写流水，实际 %d 条", n)
	}

	if err := changeOut(f, p, v, wh.ID, 6, "SO-RESTOCK-OUT-2"); err != nil {
		t.Fatalf("归还之后无限行仍应可卖（第 6 件被判库存不足即缺陷复现）：%v", err)
	}
}

func TestRestockStillIncreasesTrackedRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "跟踪商品归还")
	v := f.firstVariant(t, p.ID)

	changeInBareSKU(t, f, p, v, wh.ID, 10, v.SKUCode)
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("前置条件不成立：显式入库应切成跟踪")
	}
	if err := changeOut(f, p, v, wh.ID, 4, "SO-TRACKED-OUT"); err != nil {
		t.Fatalf("跟踪行出库失败：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 6 {
		t.Fatalf("跟踪行扣减后应为 6，实际 %d", got)
	}

	if err := restockViaOrderTx(t, f, p, v, wh.ID, 4, "order"); err != nil {
		t.Fatalf("跟踪行归还失败：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("跟踪行归还后应为 10，实际 %d", got)
	}
	if n := countMovements(t, f, v.ID); n == 0 {
		t.Fatalf("跟踪行归还应写流水")
	}
}

func TestRestockMixedBatchOnlySkipsUntracked(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)

	pInf := mustProduct(t, f, "混合商品归还-无限")
	vInf := f.firstVariant(t, pInf.ID)
	pTracked := mustProduct(t, f, "混合商品归还-跟踪")
	vTracked := f.firstVariant(t, pTracked.ID)
	changeInBareSKU(t, f, pTracked, vTracked, wh.ID, 10, vTracked.SKUCode)

	op := orderstock.New(f.inventory)
	err := f.db.Transaction(func(tx *gorm.DB) error {
		return op.ChangeStockTx(context.Background(), tx, &ordercontract.StockAdjustment{
			ProjectID: f.projectID, ReasonCode: "return_in", SourceType: "order_return", SourceRef: "SO-MIX-1",
			Lines: []ordercontract.StockLine{
				{ProductID: pInf.ID, VariantID: vInf.ID, SKUCode: vInf.SKUCode, WarehouseID: wh.ID, Quantity: 3},
				{ProductID: pTracked.ID, VariantID: vTracked.ID, SKUCode: vTracked.SKUCode, WarehouseID: wh.ID, Quantity: 3},
			},
		})
	})
	if err != nil {
		t.Fatalf("混合批量归还失败：%v", err)
	}
	if stockTrack(t, f, vInf.ID, wh.ID) {
		t.Fatalf("混合批里的无限行被切成了跟踪")
	}
	if got := f.stockQty(t, vInf.ID, wh.ID); got != 0 {
		t.Fatalf("混合批里的无限行不该被写数量，实际 %d", got)
	}
	if got := f.stockQty(t, vTracked.ID, wh.ID); got != 13 {
		t.Fatalf("混合批里的跟踪行应增加 3（10 → 13），实际 %d", got)
	}
}

func TestExplicitChangeStillSwitchesUntrackedRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "显式入库商品")
	v := f.firstVariant(t, p.ID)

	if _, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 7,
		}},
	}); err != nil {
		t.Fatalf("显式入库失败：%v", err)
	}
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("显式入库应把无限行切成跟踪（设计意图，不能被本次修复波及）")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("显式入库后数量应为 7，实际 %d", got)
	}
}
