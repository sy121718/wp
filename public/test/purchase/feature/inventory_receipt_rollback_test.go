package feature

import (
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

func TestReceiptRollsBackAccountingWhenStockChangeFails(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	source := mustPurchaseSource(t, f, "SUP_RB", "回滚供应商", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "收货回滚商品")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-RB-1", source.ID, wh.ID, []purchasedto.PurchaseLineReq{{
		VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
		Quantity: 3, UnitPrice: 4.5,
	}})

	if err := f.db.Exec("CREATE OR REPLACE FUNCTION test_block_movement() RETURNS trigger AS $$ " +
		"BEGIN RAISE EXCEPTION '故障注入：库存流水写入失败'; END $$ LANGUAGE plpgsql").Error; err != nil {
		t.Fatalf("创建故障注入函数失败: %v", err)
	}
	if err := f.db.Exec("CREATE TRIGGER test_block_movement BEFORE INSERT ON inventory_stock_movements " +
		"FOR EACH ROW EXECUTE FUNCTION test_block_movement()").Error; err != nil {
		t.Fatalf("创建故障注入触发器失败: %v", err)
	}

	if _, err := receiveLine(t, f, order.ID, order.Lines[0].ID, 3, "rb-1"); err == nil {
		t.Fatalf("库存变动失败时登记收货必须整体失败")
	}

	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 0 {
		t.Fatalf("采购行的已入库数量必须回滚到 0，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 0 {
		t.Fatalf("入库单必须回滚掉，实际 %d 张", got)
	}
	var receiptItems int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_receipt_items WHERE variant_id = ?", v.ID).
		Scan(&receiptItems).Error; err != nil {
		t.Fatalf("统计入库行失败: %v", err)
	}
	if receiptItems != 0 {
		t.Fatalf("入库行必须回滚掉，实际 %d 行", receiptItems)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusOrdered {
		t.Fatalf("采购单状态应仍是 ordered，实际 %q", got)
	}

	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("库存数量必须回滚到 0（故障发生在写回之后），实际 %d", got)
	}
	var stockRows int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		v.ID, wh.ID).Scan(&stockRows).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if stockRows != 1 {
		t.Fatalf("库存行应仍在（回滚 ≠ 没建行），实际 %d 行", stockRows)
	}
	var movements int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE variant_id = ?", v.ID).
		Scan(&movements).Error; err != nil {
		t.Fatalf("统计流水失败: %v", err)
	}
	if movements != 0 {
		t.Fatalf("库存流水必须回滚掉，实际 %d 条", movements)
	}
}
