package feature

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	"go_wp/pkg/database"
	"go_wp/public/migrations"
)

const costMigrationFile = "../../../migrations/244_inventory_stock_cost.sql"

func stockCostIn(t *testing.T, f *invFixture, variantID, warehouseID string) *float64 {
	t.Helper()
	var cost sql.NullFloat64
	if err := f.db.Raw("SELECT cost_price FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&cost).Error; err != nil {
		t.Fatalf("读仓库侧成本价失败: %v", err)
	}
	if !cost.Valid {
		return nil
	}
	value := cost.Float64
	return &value
}

func stockRowCount(t *testing.T, f *invFixture, warehouseID, skuCode string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		warehouseID, skuCode).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}

func mustStockCost(t *testing.T, f *invFixture, variantID, warehouseID string, want float64) {
	t.Helper()
	got := stockCostIn(t, f, variantID, warehouseID)
	if got == nil {
		t.Fatalf("(仓库 %s, 变体 %s) 的成本应为 %.2f，实际为 NULL（未核算）", warehouseID, variantID, want)
	}
	if *got != want {
		t.Fatalf("(仓库 %s, 变体 %s) 的成本应为 %.2f，实际 %.2f", warehouseID, variantID, want, *got)
	}
}

func TestInventoryStockCostFromReceipt(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	source := mustPurchaseSource(t, f, "SUP_A", "供应商 A", purchaseenums.SourceTypeExternal)
	p := mustProductPriced(t, f, "Tee", 99)
	v := f.firstVariant(t, p.ID)

	if got := stockCostIn(t, f, v.ID, wh.ID); got != nil {
		t.Fatalf("建变体生成的库存行不该自带成本，实际 %v", *got)
	}

	order1 := mustPurchaseOrder(t, f, "PO-COST-1", source.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 10, 12.50)})
	mustReceiveLine(t, f, order1.ID, order1.Lines[0].ID, 10, "cost-receipt-1")
	mustStockCost(t, f, v.ID, wh.ID, 12.50)

	order2 := mustPurchaseOrder(t, f, "PO-COST-2", source.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 4, 9.75)})
	mustReceiveLine(t, f, order2.ID, order2.Lines[0].ID, 4, "cost-receipt-2")
	mustStockCost(t, f, v.ID, wh.ID, 9.75)

	order3 := mustPurchaseOrder(t, f, "PO-COST-3", source.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 2, 15.25)})
	if _, err := f.purchase.RegisterReceipt(ctx, &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order3.ID, RequestID: "cost-receipt-3",
		Lines: []purchasedto.ReceiptLineReq{{
			LineID: order3.Lines[0].ID, Quantity: 2, UnitPrice: floatPtr(20.50),
		}},
	}); err != nil {
		t.Fatalf("按到货价登记入库失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 20.50)

	if got := variantCostIn(t, f, v.ID); got == nil || *got != 20.50 {
		t.Fatalf("商品侧成本价回写应保持 20.50，实际 %v", got)
	}

	if got := f.stockQty(t, v.ID, wh.ID); got != 16 {
		t.Fatalf("三次入库（10+4+2）后真源应为 16，实际 %d", got)
	}
}

func TestInventoryStockCostExplicitWritesAndUntouched(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	source := mustPurchaseSource(t, f, "SUP_B", "供应商 B", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Hoodie")
	v := f.firstVariant(t, p.ID)

	order := mustPurchaseOrder(t, f, "PO-COST-4", source.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 6, 8.25)})
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 6, "cost-receipt-4")
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 2,
		}},
	}); err != nil {
		t.Fatalf("出库 2 失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "stocktake_adjust",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 9,
		}},
	}); err != nil {
		t.Fatalf("盘点调整失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "damage_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err != nil {
		t.Fatalf("报损失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "stocktake_adjust",
		Remark: "外部核算导入",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID,
			Quantity: 8, CostPrice: floatPtr(0),
		}},
	}); err != nil {
		t.Fatalf("带显式成本的变动失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 0)

	for _, bad := range []float64{-1, -0.01} {
		if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
			ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
			Lines: []inventorydto.StockChangeLineReq{{
				VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID,
				Quantity: 1, CostPrice: floatPtr(bad),
			}},
		}); err == nil || err.Error() != inventoryenums.ErrStockCostInvalid {
			t.Fatalf("显式成本 %v 应返回 ErrStockCostInvalid，实际 %v", bad, err)
		}
	}
	nan := 0.0
	nan = nan / nan
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID,
			Quantity: 1, CostPrice: &nan,
		}},
	}); err == nil || err.Error() != inventoryenums.ErrStockCostInvalid {
		t.Fatalf("NaN 显式成本应返回 ErrStockCostInvalid，实际 %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 0)
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("被拒绝的变动不应改动真源，期望 8，实际 %d", got)
	}
}

func TestInventoryStockCostProductionInbound(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	factory := mustPurchaseSource(t, f, "FAC", "自家工厂", purchaseenums.SourceTypeInternal)
	p := mustProduct(t, f, "Mug")
	v := f.firstVariant(t, p.ID)

	if _, err := f.purchase.RegisterProductionInbound(context.Background(), &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 5, UnitCost: floatPtr(6.50), RequestID: "prod-cost-1",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 6.50)
}

func TestInventoryStockCostPerWarehouseAndSKU(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	suzhou := f.createWarehouse(t, "SZ", "苏州仓", true)
	shanghai := f.createWarehouse(t, "SH", "上海仓", false)
	source := mustPurchaseSource(t, f, "SUP_C", "供应商 C", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Cap")
	v := f.firstVariant(t, p.ID)

	bare := bareSKU(v.SKUCode, suzhou.Code)
	purchaseLines := func(quantity int, unitPrice float64) []purchasedto.PurchaseLineReq {
		return []purchasedto.PurchaseLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: bare,
			Quantity: quantity, UnitPrice: unitPrice,
		}}
	}
	orderSZ := mustPurchaseOrder(t, f, "PO-COST-SZ", source.ID, suzhou.ID, purchaseLines(3, 5.50))
	mustReceiveLine(t, f, orderSZ.ID, orderSZ.Lines[0].ID, 3, "cost-sz-1")
	orderSH := mustPurchaseOrder(t, f, "PO-COST-SH", source.ID, shanghai.ID, purchaseLines(7, 7.75))
	mustReceiveLine(t, f, orderSH.ID, orderSH.Lines[0].ID, 7, "cost-sh-1")

	mustStockCost(t, f, v.ID, suzhou.ID, 5.50)
	mustStockCost(t, f, v.ID, shanghai.ID, 7.75)

	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: shanghai.ID})
	if err != nil {
		t.Fatalf("读库存记录失败: %v", err)
	}
	if row.CostPrice == nil || *row.CostPrice != 7.75 {
		t.Fatalf("库存记录应带出该仓成本 7.75，实际 %v", row.CostPrice)
	}

	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: bare,
	})
	if err != nil {
		t.Fatalf("按 SKU 读各仓库存失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("该 SKU 应有 2 条仓库维度记录，实际 %d", len(rows))
	}
	wantCost := map[string]float64{suzhou.ID: 5.50, shanghai.ID: 7.75}
	for _, r := range rows {
		want, ok := wantCost[r.WarehouseID]
		if !ok {
			t.Fatalf("出现未预期的仓库 %s", r.WarehouseID)
		}
		if r.SKUCode != bare {
			t.Fatalf("库存行的 SKU 应是仓库侧裸码 %q，实际 %q", bare, r.SKUCode)
		}
		if r.CostPrice == nil || *r.CostPrice != want {
			t.Fatalf("仓库 %s 的成本应为 %.2f，实际 %v", r.WarehouseCode, want, r.CostPrice)
		}
	}

	list, err := f.inventory.ListStocks(ctx, &inventorydto.ListStockReq{
		ProjectID: f.projectID, VariantID: v.ID,
	})
	if err != nil {
		t.Fatalf("读库存列表失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("库存列表应有 2 行，实际 %d", len(list))
	}
	for _, r := range list {
		if r.CostPrice == nil {
			t.Fatalf("库存列表的每一行都应带成本（仓库 %s）", r.WarehouseCode)
		}
	}
}

func TestInventoryStockWarehouseSkuUniqueRejectsDuplicate(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}

	var columnCount int
	if err := f.db.Raw("SELECT COUNT(*) FROM information_schema.columns " +
		"WHERE table_schema = current_schema() AND table_name = 'inventory_stocks' " +
		"AND column_name = 'cost_price'").Scan(&columnCount).Error; err != nil {
		t.Fatalf("查列失败: %v", err)
	}
	if columnCount != 1 {
		t.Fatalf("inventory_stocks.cost_price 应存在（迁移 244），实际 %d 列", columnCount)
	}
	var constraintCount int
	if err := f.db.Raw("SELECT COUNT(*) FROM pg_constraint " +
		"WHERE conrelid = 'inventory_stocks'::regclass " +
		"AND conname = 'uq_inventory_stocks_warehouse_sku' AND contype = 'u' AND convalidated").
		Scan(&constraintCount).Error; err != nil {
		t.Fatalf("查约束失败: %v", err)
	}
	if constraintCount != 1 {
		t.Fatalf("uq_inventory_stocks_warehouse_sku 应存在且已验证（迁移 244），实际 %d", constraintCount)
	}

	f.createWarehouse(t, "SZ", "苏州仓", true)
	shanghai := f.createWarehouse(t, "SH", "上海仓", false)
	ningbo := f.createWarehouse(t, "NB", "宁波仓", false)
	p1 := mustProduct(t, f, "Alpha")
	p2 := mustProduct(t, f, "Beta")
	v1 := f.firstVariant(t, p1.ID)
	v2 := f.firstVariant(t, p2.ID)
	const dupSKU = "DUP-SKU-1"

	insertRow := func(warehouseID, productID, variantID string) error {
		return f.db.Exec("INSERT INTO inventory_stocks (project_id, warehouse_id, product_id, variant_id, sku_code) "+
			"VALUES (?,?,?,?,?)", f.projectID, warehouseID, productID, variantID, dupSKU).Error
	}
	if err := insertRow(shanghai.ID, p1.ID, v1.ID); err != nil {
		t.Fatalf("首行插入应成功: %v", err)
	}
	if got := stockRowCount(t, f, shanghai.ID, dupSKU); got != 1 {
		t.Fatalf("(SH, %s) 应有 1 行，实际 %d", dupSKU, got)
	}

	err := insertRow(shanghai.ID, p2.ID, v2.ID)
	if err == nil {
		t.Fatalf("(SH, %s) 的第二行必须被 UNIQUE (warehouse_id, sku_code) 拒绝", dupSKU)
	}
	if !database.IsUniqueViolation(err) {
		t.Fatalf("同仓同 SKU 的重复插入应是唯一键冲突（23505），实际 %v", err)
	}
	if got := stockRowCount(t, f, shanghai.ID, dupSKU); got != 1 {
		t.Fatalf("被拒绝的插入不应留下数据，(SH, %s) 实际 %d 行", dupSKU, got)
	}

	if err := insertRow(ningbo.ID, p2.ID, v2.ID); err != nil {
		t.Fatalf("同一 SKU 落到不同仓应允许: %v", err)
	}
	if got := stockRowCount(t, f, ningbo.ID, dupSKU); got != 1 {
		t.Fatalf("(NB, %s) 应有 1 行，实际 %d", dupSKU, got)
	}
}

func TestInventoryStockCostMigrationShape(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	raw, err := os.ReadFile(costMigrationFile)
	if err != nil {
		t.Fatalf("读迁移 244 失败（%s）: %v", costMigrationFile, err)
	}
	sqlText := string(raw)

	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS cost_price",
		"RAISE EXCEPTION",
		"duplicate_object",
		"uq_inventory_stocks_warehouse_sku",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("迁移 244 应包含 %q", want)
		}
	}
	scan := strings.Index(sqlText, "RAISE EXCEPTION")
	guard := strings.Index(sqlText, "ADD CONSTRAINT uq_inventory_stocks_warehouse_sku")
	if scan < 0 || guard < 0 || scan > guard {
		t.Fatalf("迁移 244 必须**先**扫存量重复（RAISE EXCEPTION）**再**建唯一约束：scan=%d guard=%d", scan, guard)
	}

	if !strings.Contains(sqlText, "sku_code") || !strings.Contains(sqlText, "warehouse_id") {
		t.Fatalf("迁移 244 的重复报告必须带 warehouse_id / sku_code 样例")
	}
}

func TestInventoryStockCostI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		inventoryenums.ErrStockCostInvalid,
		"admin.inventory.cost.unknown",
	}
	for _, key := range keys {
		for _, lang := range []string{"zh-CN", "en-US"} {
			var n int
			if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n "+
				"WHERE item_key = ? AND lang = ? AND item_value <> ''", key, lang).Scan(&n).Error; err != nil {
				t.Fatalf("查词条 %s/%s 失败: %v", key, lang, err)
			}
			if n != 1 {
				t.Fatalf("词条 %s/%s 应有且仅有一行非空文案，实际 %d", key, lang, n)
			}
		}
	}
}

func TestInventoryWarehouseDefaultUndeletable(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	fallback := f.createWarehouse(t, "SZ", "苏州仓", true)
	spare := f.createWarehouse(t, "SH", "上海仓", false)

	err := f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{
		ID: fallback.ID, ProjectID: f.projectID,
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("删除默认仓应返回 ErrWarehouseIsDefault，实际 %v", err)
	}
	if _, gerr := f.inventory.GetWarehouse(ctx, &inventorydto.GetWarehouseReq{
		ID: fallback.ID, ProjectID: f.projectID,
	}); gerr != nil {
		t.Fatalf("默认仓必须还在: %v", gerr)
	}

	if err = f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{
		ID: spare.ID, ProjectID: f.projectID,
	}); err != nil {
		t.Fatalf("删除非默认仓应成功，实际 %v", err)
	}
	if _, gerr := f.inventory.GetWarehouse(ctx, &inventorydto.GetWarehouseReq{
		ID: spare.ID, ProjectID: f.projectID,
	}); gerr == nil {
		t.Fatalf("非默认仓删除后应读不到")
	}
}

func TestInventoryStockCostDuplicateScanRaises(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p1 := mustProduct(t, f, "Gamma")
	p2 := mustProduct(t, f, "Delta")
	v1 := f.firstVariant(t, p1.ID)
	v2 := f.firstVariant(t, p2.ID)

	if err := f.db.Exec("ALTER TABLE inventory_stocks DROP CONSTRAINT uq_inventory_stocks_warehouse_sku").Error; err != nil {
		t.Fatalf("拆约束失败（模拟存量库）: %v", err)
	}

	dupSKU := bareSKU(v1.SKUCode, wh.Code)
	if err := f.db.Exec("UPDATE inventory_stocks SET sku_code = ? WHERE variant_id = ? AND warehouse_id = ?",
		dupSKU, v2.ID, wh.ID).Error; err != nil {
		t.Fatalf("造重复行失败: %v", err)
	}
	if got := stockRowCount(t, f, wh.ID, dupSKU); got != 2 {
		t.Fatalf("造重复应得到 2 行，实际 %d", got)
	}

	err := migrations.Run(f.db)
	if err == nil {
		t.Fatalf("存量重复时迁移必须失败，实际成功了（静默放过 = 之后建索引报一个看不懂的 23505）")
	}
	msg := err.Error()
	for _, want := range []string{"同仓同 SKU 的多行", "warehouse_id=", "sku_code="} {
		if !strings.Contains(msg, want) {
			t.Fatalf("迁移报错应带定位信息 %q，实际: %v", want, err)
		}
	}

	if got := stockRowCount(t, f, wh.ID, bareSKU(v1.SKUCode, wh.Code)); got != 2 {
		t.Fatalf("报错不应改动数据，实际 %d 行", got)
	}
}
