package feature

// inventory_receipt_rollback_test.go — 故障注入：库存变动在**事务中途**失败时，
// 收货这一次写操作必须整体回滚。
//
// 要证明的是本次收口的核心命题：记账（采购行已入库数量 / 入库单 / 入库行 / 采购单状态）
// 与库存变动（数量写回 / 流水 / 成本）在同一条事务里，任一步失败都不留半截状态。
// 收口之前它们是两段：记账事务先提交，随后另开事务动库存，失败靠
// `s.compensateReceipt(...)` 补偿（而调用处是 `_ =` 吞错）——
// 补偿再失败就是「记了账没动库存」，错误还被丢掉。
//
// 注入点刻意选在库存事务的**最后一步**（写流水）：故障发生时库存数量已经 UPDATE 过，
// 因此回滚必须把数量一起退回去 —— 只断言「单据没了」是不够的。

import (
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

func TestReceiptRollsBackAccountingWhenStockChangeFails(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	source := mustPurchaseSource(t, f, "SUP_RB", "回滚供应商", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "收货回滚商品")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-RB-1", source.ID, wh.ID, []inventorydto.PurchaseLineReq{{
		VariantID: v.ID, ProductID: p.ID, SKUCode: bareSKU(v.SKUCode, wh.Code),
		Quantity: 3, UnitPrice: 4.5,
	}})

	// 故障注入：让库存流水的 INSERT 直接抛异常。触发器只存在于本用例的隔离 schema 里，
	// 不碰任何生产迁移；换成「库存服务不可用」这类外部故障时，前几步同样已经写过了。
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

	// 逐项断言「一条都不该留下」。
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
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusPending {
		t.Fatalf("采购单状态应仍是 pending，实际 %q", got)
	}
	// 库存：行还在（商品建变体时生成的那一行），但数量与流水都要退回去 ——
	// 故障发生在数量写回之后，这里最能说明「同一条事务」。
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
