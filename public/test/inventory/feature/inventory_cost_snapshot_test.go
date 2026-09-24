// Package feature inventory 模块 feature 测试 —— 成本快照口径收口（2026-09-19 商品域评审）。
//
// 口径（docs/14-product-sku-and-cost-model.md §1.3 / §4.2 / §4.3 / §9.3）：
//
//	· 归属仓成本 = 该变体在**归属仓**（行上显式仓库，为空按既有解析规则取默认仓）
//	  那一行 inventory_stocks 的当前成本；未核算（NULL）返回 nil —— 绝不用 0 冒充
//	  （0 是合法的显式成本：赠品 / 内部划拨）；
//	· 出库方向的流水在写入时从该库存行复制当时的 unit_cost，之后改库存行成本
//	  **不影响已写流水**；
//	· VariantHasStockMovement 是变体删除守卫（有流水 = 被订单用过）。
//
// 断言一律直查真源列（inventory_stocks.cost_price / inventory_stock_movements.unit_cost），
// 不走 service 自己返回的响应 —— 那只能证明「service 以为自己写了」。
package feature

import (
	"context"
	"database/sql"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"

	"go_wp/public/migrations"
)

// movementUnitCostOfBatch 直读真源：某批次流水的成本留痕（nil = 当时未核算）。
func movementUnitCostOfBatch(t *testing.T, f *invFixture, batchID string) *float64 {
	t.Helper()
	var cost sql.NullFloat64
	if err := f.db.Raw("SELECT unit_cost FROM inventory_stock_movements WHERE batch_id = ?",
		batchID).Scan(&cost).Error; err != nil {
		t.Fatalf("读流水 unit_cost 失败: %v", err)
	}
	if !cost.Valid {
		return nil
	}
	value := cost.Float64
	return &value
}

// setStockCostByChange 给某 (仓库, 变体) 写当前成本（走真实入库路径的显式成本）。
//
// 显式成本只在**入库**方向被接受（出库 / 盘点不动成本），所以这里入库 1 件：
// 数量变了才有流水，成本与数量落在同一个事务里（迁移 244）。
func setStockCostByChange(t *testing.T, f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp,
	warehouseID string, cost float64) string {
	t.Helper()
	res, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		SourceType: "manual", SourceRef: "PO-COST",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
			WarehouseID: warehouseID, Quantity: 1, CostPrice: floatPtr(cost),
		}},
	})
	if err != nil {
		t.Fatalf("写成本（入库 1 件 %v）失败: %v", cost, err)
	}
	return res.BatchID
}

// TestResolveVariantWarehouseCostsBothPaths 验收 ①③：
// 显式 WarehouseID 与「按默认仓解析」两条路径都取对；未核算返回 nil 而不是 0。
func TestResolveVariantWarehouseCostsBothPaths(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	def := f.createWarehouse(t, "SZ", "默认仓", true)
	second := f.createWarehouse(t, "SH", "第二仓", false)

	p := mustProduct(t, f, "CostPath")
	v := f.firstVariant(t, p.ID)
	// 两个仓各写一个不同的成本：显式仓与默认仓的取值必须能区分开。
	setStockCostByChange(t, f, p, v, def.ID, 3.50)
	setStockCostByChange(t, f, p, v, second.ID, 7.20)
	// 第二个变体：订单还没核算过（库存行存在但成本为 NULL）。
	other := mustSecondVariant(t, f, p.ID)

	got, err := f.inventory.ResolveVariantWarehouseCosts(ctx, f.projectID, []inventorydto.VariantWarehouseCostRef{
		{VariantID: v.ID},
		{VariantID: v.ID, WarehouseID: second.ID},
		{VariantID: other.ID},
	})
	if err != nil {
		t.Fatalf("解析归属仓成本失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应返回 3 行结果，实际 %d", len(got))
	}
	if got[0].WarehouseID != def.ID {
		t.Fatalf("留空应解析到默认仓 %s，实际 %s", def.ID, got[0].WarehouseID)
	}
	if got[0].CostPrice == nil || *got[0].CostPrice != 3.50 {
		t.Fatalf("默认仓成本应为 3.50，实际 %v", got[0].CostPrice)
	}
	if got[1].WarehouseID != second.ID {
		t.Fatalf("显式仓应原样返回 %s，实际 %s", second.ID, got[1].WarehouseID)
	}
	if got[1].CostPrice == nil || *got[1].CostPrice != 7.20 {
		t.Fatalf("显式仓成本应为 7.20，实际 %v", got[1].CostPrice)
	}
	// 未核算：nil（**不是 0**）—— 0 是合法的显式成本，两者混在一起利润就错了。
	if got[2].CostPrice != nil {
		t.Fatalf("未核算的变体应返回 nil，实际 %v", *got[2].CostPrice)
	}
}

// TestMovementUnitCostIsSnapshotOfOutboundCost 验收 ⑤：
// 出库流水记下当时的成本；之后改库存行成本**不影响**已写流水。
func TestMovementUnitCostIsSnapshotOfOutboundCost(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "默认仓", true)
	p := mustProduct(t, f, "CostTrace")
	v := f.firstVariant(t, p.ID)

	// ① 入库 10 件并把成本定为 5.00：入库流水记的正是这批货进来的成本。
	inRes, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		SourceType: "manual", SourceRef: "PO-10",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
			WarehouseID: wh.ID, Quantity: 10, CostPrice: floatPtr(5.00),
		}},
	})
	if err != nil {
		t.Fatalf("入库 10 件（带成本 5.00）失败: %v", err)
	}
	if got := movementUnitCostOfBatch(t, f, inRes.BatchID); got == nil || *got != 5.00 {
		t.Fatalf("入库流水的 unit_cost 应为本次显式成本 5.00，实际 %v", got)
	}

	// ② 出库 3 件：成本不动，流水复制当时的库存行成本 5.00。
	outRes, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "sale_out",
		SourceType: "order", SourceRef: "SO-1",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
			WarehouseID: wh.ID, Quantity: 3,
		}},
	})
	if err != nil {
		t.Fatalf("出库 3 件失败: %v", err)
	}
	if got := movementUnitCostOfBatch(t, f, outRes.BatchID); got == nil || *got != 5.00 {
		t.Fatalf("出库流水的 unit_cost 应为扣减时的库存行成本 5.00，实际 %v", got)
	}

	// ③ 改库存行成本到 9.00（入库显式成本覆盖式），已写流水的留痕必须原样不动。
	setStockCostByChange(t, f, p, v, wh.ID, 9.00)
	if got := stockCostIn(t, f, v.ID, wh.ID); got == nil || *got != 9.00 {
		t.Fatalf("库存行成本应已被改为 9.00，实际 %v", got)
	}
	if got := movementUnitCostOfBatch(t, f, outRes.BatchID); got == nil || *got != 5.00 {
		t.Fatalf("改价后历史出库流水的 unit_cost 必须仍是 5.00，实际 %v", got)
	}
}

// TestCostSnapshotMigrationAppliesIdempotently 迁移 256 的结构与幂等：
// CheckSQL 判的是「列已可空且无默认值 / unit_cost 列已存在」——因此在**已经跑过一遍**
// 的库上重跑必须整体跳过、不留半改状态（只判「列存在」会在「列在但仍是 NOT NULL
// DEFAULT 0」时静默跳过，DB-015 的坑）。
func TestCostSnapshotMigrationAppliesIdempotently(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	// 模板库已按生产迁移建成（含 256）：在它上面再跑一次全部迁移。
	if err := migrations.Run(f.db); err != nil {
		t.Fatalf("迁移重跑失败（幂等性被破坏）: %v", err)
	}
	var nullable string
	var hasDefault bool
	if err := f.db.Raw("SELECT is_nullable, (column_default IS NOT NULL) FROM information_schema.columns "+
		"WHERE table_schema = current_schema() AND table_name = 'order_items' AND column_name = 'cost_price'").
		Row().Scan(&nullable, &hasDefault); err != nil {
		t.Fatalf("读 order_items.cost_price 结构失败: %v", err)
	}
	if nullable != "YES" || hasDefault {
		t.Fatalf("order_items.cost_price 应为可空且无默认值，实际 nullable=%s default=%v", nullable, hasDefault)
	}
	var unitCostType string
	if err := f.db.Raw("SELECT data_type FROM information_schema.columns " +
		"WHERE table_schema = current_schema() AND table_name = 'inventory_stock_movements' AND column_name = 'unit_cost'").
		Row().Scan(&unitCostType); err != nil {
		t.Fatalf("读 inventory_stock_movements.unit_cost 结构失败: %v", err)
	}
	if unitCostType != "numeric" {
		t.Fatalf("inventory_stock_movements.unit_cost 应为 numeric，实际 %s", unitCostType)
	}
}

// TestVariantHasStockMovementGuard 验收 ⑥：有流水 / 无流水的变体分别返回真 / 假。
func TestVariantHasStockMovementGuard(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "默认仓", true)
	p := mustProduct(t, f, "Guard")
	used := f.firstVariant(t, p.ID)
	untouched := mustSecondVariant(t, f, p.ID)

	// 没用过：建变体会生成一条 0 库存记录，但**没有流水** —— 守卫必须返回假。
	if got, err := f.inventory.VariantHasStockMovement(ctx, f.projectID, untouched.ID); err != nil || got {
		t.Fatalf("没有流水的变体应返回 false，实际 %v（err=%v）", got, err)
	}

	changeIn(t, f, p, used, wh.ID, 4, "purchase_in")

	usedHas, err := f.inventory.VariantHasStockMovement(ctx, f.projectID, used.ID)
	if err != nil {
		t.Fatalf("查流水失败: %v", err)
	}
	if !usedHas {
		t.Fatalf("有过流水的变体应返回 true")
	}
	// 另一个变体仍未被用过（守卫不能按「这张表有没有流水」粗判）。
	if got, err := f.inventory.VariantHasStockMovement(ctx, f.projectID, untouched.ID); err != nil || got {
		t.Fatalf("另一个变体应仍为 false，实际 %v（err=%v）", got, err)
	}
}
