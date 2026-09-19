// Package feature inventory 模块 feature 测试 —— 仓库侧成本与库存身份收口（批次 A）。
//
// 口径（docs/14-product-sku-and-cost-model.md §4，用户 2026-09-19 逐条确认）：
//
//	· 成本写到**仓库侧**：只记一个当前成本价、不做成本流水，(仓库, SKU) 与库存同维度；
//	· cost_price 可空：NULL = 尚未核算 —— 绝不用 0 冒充「未知成本」
//	  （0 是合法的显式成本：赠品 / 内部划拨）；
//	· 采购入库的单价即该 (仓库, SKU) 的当前成本（覆盖式，最近一次为准）；
//	  其它入库 / 出库 / 调整不动成本，除非显式传成本；
//	· **仓库内唯一**：同一仓库不能有重复 SKU（UNIQUE (warehouse_id, sku_code)，
//	  迁移 244），同一条迁移在建约束之前先扫存量重复并带样例报错；
//	· 默认仓不可删除（给「暂时不知道怎么处理」的商品兜底）。
//
// 断言一律直查真源列（inventory_stocks.cost_price / pg_constraint / sys_i18n），
// 不走 service 自己返回的响应 —— 那只能证明「service 以为自己写了」。
package feature

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	"go_wp/pkg/database"
	"go_wp/public/migrations"
)

// costMigrationFile 迁移 244 的 SQL 路径（相对本包目录）：
// public/test/inventory/feature → ../../../migrations = public/migrations。
const costMigrationFile = "../../../migrations/244_inventory_stock_cost.sql"

// stockCostIn 直读真源里的仓库侧成本价（nil = 尚未核算，与 0 严格区分）。
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

// stockRowCount 直读真源里某 (仓库, SKU) 的行数（唯一性的反向证据）。
func stockRowCount(t *testing.T, f *invFixture, warehouseID, skuCode string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		warehouseID, skuCode).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}

// mustStockCost 断言成本存在且等于期望值（取值都为二进制可精确表示的两位小数）。
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

// TestInventoryStockCostFromReceipt 采购入库把单价写为该 (仓库, SKU) 的当前成本（覆盖式）。
//
// 四条：① 入库前为 NULL（尚未核算，不是 0）；② 入库后等于采购单价；
// ③ 同一 (仓库, SKU) 再入一批不同单价时**覆盖**（最近一次为准，不做加权、不做流水）；
// ④ 本批显式到货价优先于采购行单价（商品侧的兼容回写同时到位）。
func TestInventoryStockCostFromReceipt(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	source := mustPurchaseSource(t, f, "SUP_A", "供应商 A", inventoryenums.SourceTypeExternal)
	p := mustProductPriced(t, f, "Tee", 99)
	v := f.firstVariant(t, p.ID)

	// ① 从未核算过：NULL。这里必须能区分「没有成本」与「成本为 0」。
	if got := stockCostIn(t, f, v.ID, wh.ID); got != nil {
		t.Fatalf("建变体生成的库存行不该自带成本，实际 %v", *got)
	}

	// ② 收 10 件 × 12.50 → 当前成本 12.50。
	order1 := mustPurchaseOrder(t, f, "PO-COST-1", source.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 10, 12.50)})
	mustReceiveLine(t, f, order1.ID, order1.Lines[0].ID, 10, "cost-receipt-1")
	mustStockCost(t, f, v.ID, wh.ID, 12.50)

	// ③ 再收一批 × 9.75：**覆盖**成最近一次，而不是加权 / 留两条历史。
	order2 := mustPurchaseOrder(t, f, "PO-COST-2", source.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 4, 9.75)})
	mustReceiveLine(t, f, order2.ID, order2.Lines[0].ID, 4, "cost-receipt-2")
	mustStockCost(t, f, v.ID, wh.ID, 9.75)

	// ④ 本批到货价（20.50）覆盖采购行单价（15.25）：收货行显式给的价就是成本。
	order3 := mustPurchaseOrder(t, f, "PO-COST-3", source.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 2, 15.25)})
	if _, err := f.inventory.RegisterReceipt(ctx, &inventorydto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order3.ID, RequestID: "cost-receipt-3",
		Lines: []inventorydto.ReceiptLineReq{{
			LineID: order3.Lines[0].ID, Quantity: 2, UnitPrice: floatPtr(20.50),
		}},
	}); err != nil {
		t.Fatalf("按到货价登记入库失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 20.50)
	// 商品侧的兼容回写（VariantCostPort）不受本批影响，仍是同一单价。
	if got := variantCostIn(t, f, v.ID); got == nil || *got != 20.50 {
		t.Fatalf("商品侧成本价回写应保持 20.50，实际 %v", got)
	}

	// 成本与库存落在同一行上：数量是真源的真值，成本也在。
	if got := f.stockQty(t, v.ID, wh.ID); got != 16 {
		t.Fatalf("三次入库（10+4+2）后真源应为 16，实际 %d", got)
	}
}

// TestInventoryStockCostExplicitWritesAndUntouched 出库 / 盘点 / 报损不动成本；
// 显式传成本才写；非法显式成本被拒绝且真源一字不动。
func TestInventoryStockCostExplicitWritesAndUntouched(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	source := mustPurchaseSource(t, f, "SUP_B", "供应商 B", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Hoodie")
	v := f.firstVariant(t, p.ID)

	// 先由采购入库写下一个基准成本 8.25。
	order := mustPurchaseOrder(t, f, "PO-COST-4", source.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 6, 8.25)})
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 6, "cost-receipt-4")
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	// ① 出库（订单侧的销售出库路径）：只动数量，成本原样。
	if _, err := f.inventory.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 2,
		}},
	}); err != nil {
		t.Fatalf("出库 2 失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	// ② 盘点调整到 9（后台「库存调整」走的就是这条路径）：成本仍然不动。
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in") // 先把可用量抬上去，便于盘点
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "stocktake_adjust",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 9,
		}},
	}); err != nil {
		t.Fatalf("盘点调整失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	// ③ 报损（out + damage_out）同样不动成本。
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "damage_out",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 1,
		}},
	}); err != nil {
		t.Fatalf("报损失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 8.25)

	// ④ 显式成本：数量没变化也照写（外部核算后导入的场景），0 是合法的显式成本。
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
	mustStockCost(t, f, v.ID, wh.ID, 0) // 0 写进去了，与 NULL 是两回事

	// ⑤ 非法显式成本：负数 / NaN 一律拒绝，且真源保持不变（连数量也不动）。
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

// TestInventoryStockCostProductionInbound 自家工厂生产入库的手工成本同样是**显式成本**，
// 与采购收货共用同一条写入路径（(仓库, SKU) 的当前值）。
func TestInventoryStockCostProductionInbound(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	factory := mustPurchaseSource(t, f, "FAC", "自家工厂", inventoryenums.SourceTypeInternal)
	p := mustProduct(t, f, "Mug")
	v := f.firstVariant(t, p.ID)

	if _, err := f.inventory.RegisterProductionInbound(context.Background(), &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 5, UnitCost: floatPtr(6.50), RequestID: "prod-cost-1",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	mustStockCost(t, f, v.ID, wh.ID, 6.50)
}

// TestInventoryStockCostPerWarehouseAndSKU 成本是 (仓库, SKU) 维度：
// 同一个 SKU 在多个仓各有自己的当前成本，互不覆盖；三条读路径都带出成本。
func TestInventoryStockCostPerWarehouseAndSKU(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	suzhou := f.createWarehouse(t, "SZ", "苏州仓", true)
	shanghai := f.createWarehouse(t, "SH", "上海仓", false)
	source := mustPurchaseSource(t, f, "SUP_C", "供应商 C", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Cap")
	v := f.firstVariant(t, p.ID)

	// 同一个 SKU 在两个仓各来一批，单价不同。
	//
	// 采购行上的 SKU 走**仓库侧裸码**（仓库里的货不带仓码前缀，前缀只留在商品 / 变体侧）：
	// 第二个仓的库存行就是这条入库路径新建的，它存下来的编码就是这个值。
	bare := bareSKU(v.SKUCode, suzhou.Code)
	purchaseLines := func(quantity int, unitPrice float64) []inventorydto.PurchaseLineReq {
		return []inventorydto.PurchaseLineReq{{
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

	// 读入口一：单条库存记录（变体 × 仓库）带成本。
	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: shanghai.ID})
	if err != nil {
		t.Fatalf("读库存记录失败: %v", err)
	}
	if row.CostPrice == nil || *row.CostPrice != 7.75 {
		t.Fatalf("库存记录应带出该仓成本 7.75，实际 %v", row.CostPrice)
	}

	// 读入口二：某 SKU 在各仓的库存，逐行带各自仓的成本。
	// 查询维度是仓库侧裸码（与库存行存的值同一口径）。
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

	// 读入口三：库存列表同样带成本（投影里就有，不必再回查）。
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

// TestInventoryStockWarehouseSkuUniqueRejectsDuplicate 仓库内唯一（迁移 244）：
// 同一仓库的第二行同 SKU **必须**被唯一约束拒绝；同一个 SKU 落在不同仓则是允许的。
func TestInventoryStockWarehouseSkuUniqueRejectsDuplicate(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	// 迁移必须真的落库：列 + 约束都在（只判列会在「列已加、约束没建成」时静默放过）。
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

	// 三个仓：SZ 是默认仓（变体建行的地方，工程内第一个仓自动成为默认仓），
	// 目标仓 SH / NB 里两行都不存在，便于直接构造重复。
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

	// 同仓同 SKU 的第二行（不同变体）：唯一约束必须拦住，且是唯一键冲突。
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

	// 同一个 SKU 文本落在**另一个仓**：允许（仓库内唯一，不是全局唯一）。
	if err := insertRow(ningbo.ID, p2.ID, v2.ID); err != nil {
		t.Fatalf("同一 SKU 落到不同仓应允许: %v", err)
	}
	if got := stockRowCount(t, f, ningbo.ID, dupSKU); got != 1 {
		t.Fatalf("(NB, %s) 应有 1 行，实际 %d", dupSKU, got)
	}
}

// TestInventoryStockCostMigrationShape 迁移 244 的形状：
// 先扫存量重复并**显式报错**，再建唯一约束；两条路径都幂等。
//
// 这条断言守的是「不得静默失败、也不得留一个没有上下文的 23505」——
// 顺序反了（先建索引后扫描）会让存量重复的库只拿到一个看不出是哪两行的错误。
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
		"ADD COLUMN IF NOT EXISTS cost_price", // 幂等加列
		"RAISE EXCEPTION",                     // 存量重复必须显式报错
		"duplicate_object",                    // 约束重跑不失败
		"uq_inventory_stocks_warehouse_sku",   // 约束名与注册 / 断言一致
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
	// 报错必须带上定位信息（哪几行），否则运维只能对着索引名猜。
	if !strings.Contains(sqlText, "sku_code") || !strings.Contains(sqlText, "warehouse_id") {
		t.Fatalf("迁移 244 的重复报告必须带 warehouse_id / sku_code 样例")
	}
}

// TestInventoryStockCostI18nSeeded 本批新增的取词位（迁移 245）中英各一行且非空。
//
// enums 常量值就是 i18n key：漏了词条，页面上会原样显示 ErrStockCostInvalid 这种裸 key；
// 漏了 en-US 不会报错，只会让英文界面回落中文 —— 那种缺陷只能等用户看见。
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

// TestInventoryWarehouseDefaultUndeletable 默认仓是「暂时不知道怎么处理」的商品的兜底：
// 删除一律被拒（明确 enums 错误），非默认仓可以正常删除。
func TestInventoryWarehouseDefaultUndeletable(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	fallback := f.createWarehouse(t, "SZ", "苏州仓", true)
	spare := f.createWarehouse(t, "SH", "上海仓", false)

	// 默认仓：拒绝，且错误是明确的业务错误（不是数据库层的外键 / 空指针）。
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

	// 非默认仓：可以删，且删完确实读不到。
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

// TestInventoryStockCostDuplicateScanRaises 存量重复时必须**显式报错并带样例**。
//
// 造法与真实升级场景一致：约束尚未建立（这里先把它拆掉模拟存量库），
// 同一仓库里已经有两行同 SKU —— 重跑迁移必须失败，且错误里能看到
// warehouse_id / sku_code / 行 id，而不是一个没有上下文的 23505，
// 更不是静默丢数据。
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

	// 模拟存量库：约束还没建（拆掉），而同一个仓里两条 SKU 已经被写成一样。
	if err := f.db.Exec("ALTER TABLE inventory_stocks DROP CONSTRAINT uq_inventory_stocks_warehouse_sku").Error; err != nil {
		t.Fatalf("拆约束失败（模拟存量库）: %v", err)
	}
	// 撞的必须是**同一个仓库侧编码**：库存行里存的是裸码（商品侧 v1.SKUCode 带仓码前缀）。
	dupSKU := bareSKU(v1.SKUCode, wh.Code)
	if err := f.db.Exec("UPDATE inventory_stocks SET sku_code = ? WHERE variant_id = ? AND warehouse_id = ?",
		dupSKU, v2.ID, wh.ID).Error; err != nil {
		t.Fatalf("造重复行失败: %v", err)
	}
	if got := stockRowCount(t, f, wh.ID, dupSKU); got != 2 {
		t.Fatalf("造重复应得到 2 行，实际 %d", got)
	}

	// 重跑迁移：CheckSQL 判定「约束不在位」→ 执行 244 → 扫描命中重复 → 显式失败。
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
	// 报错前后数据一字不动：不做「先删重复再建索引」这种丢数据的处理。
	if got := stockRowCount(t, f, wh.ID, bareSKU(v1.SKUCode, wh.Code)); got != 2 {
		t.Fatalf("报错不应改动数据，实际 %d 行", got)
	}
}
