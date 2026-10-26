// inventory_stock_reserve_test.go — 库存货行建档（迁移 624 / 2026-10 去认领）与预占释放。
//
// 覆盖的会红断言：
//
//	4  货行建档：同仓同裸码插第二行撞 uq_inventory_stocks_warehouse_sku（提前拦成业务错误）
//	7  预占不写流水
//	8  可售不足整单拒绝（第一行 reserved 回到原值）
//	9  并发预占不超卖（-race）
//	10 无归属行不进商品侧聚合（只按变体引用的货行清单算）
//	11 删仓守卫仍拦得住无归属存货
//	12 建档的自相矛盾输入被拒（数量 > 0 却不跟踪）
//
// 全部走真实 PG + 生产迁移 + 真实 service（不手抄 CREATE TABLE）。
package feature

import (
	"context"
	"errors"
	"sync"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	productmodel "go_wp/internal/module/product/model"
)

// createStock 建一条无归属（尚未挂到任何商品变体）的货行，走 service 的真实建档路径。
func createStock(t *testing.T, f *invFixture, warehouseID, sku string, qty int, track bool) *inventorydto.StockResp {
	t.Helper()
	res, err := f.inventory.CreateStock(context.Background(), &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID, SKUCode: sku,
		Quantity: qty, TrackQuantity: track,
	})
	if err != nil {
		t.Fatalf("建货行 %s 失败: %v", sku, err)
	}
	return res
}

// stockReserved 直读真源表的预占数量（断言落库结果，不经 service）。
func (f *invFixture) stockReserved(t *testing.T, variantID, warehouseID string) int {
	t.Helper()
	var reserved int
	if err := f.db.Raw("SELECT reserved_quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&reserved).Error; err != nil {
		t.Fatalf("读预占数量失败: %v", err)
	}
	return reserved
}

// movementCount 流水总条数（预占 / 释放不写流水，靠它断言）。
func (f *invFixture) movementCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE project_id = ?",
		f.projectID).Scan(&n).Error; err != nil {
		t.Fatalf("读流水条数失败: %v", err)
	}
	return n
}

// TestStockCreateAndDuplicateCode 验收 4：
// 无归属行（variant_id / product_id 均 NULL）可建；同仓同裸码插第二行被拒。
func TestStockCreateAndDuplicateCode(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	row := createStock(t, f, wh.ID, "DRAWERSMOKE_001", 10, true)
	if row.Quantity != 10 || row.ReservedQuantity != 0 || row.AvailableQuantity != 10 {
		t.Fatalf("建档后应是 10 件、预占 0、可售 10，实际 %d/%d/%d",
			row.Quantity, row.ReservedQuantity, row.AvailableQuantity)
	}
	// 落库路径也要一致：NULL 必须是 NULL，不是空串。
	var variantID, productID *string
	if err := f.db.Raw("SELECT variant_id, product_id FROM inventory_stocks WHERE id = ?", row.ID).
		Row().Scan(&variantID, &productID); err != nil {
		t.Fatalf("读货行失败: %v", err)
	}
	if variantID != nil || productID != nil {
		t.Fatalf("无归属行的 variant_id / product_id 必须是 NULL，实际 %v / %v", variantID, productID)
	}

	// 同仓同裸码第二行：提前拦截成可读的业务错误（不是 23505）。
	if _, err := f.inventory.CreateStock(context.Background(), &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "DRAWERSMOKE_001",
		Quantity: 3, TrackQuantity: true,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrWarehouseSKUCodeTaken)) {
		t.Fatalf("同仓同码应被拒为 ErrWarehouseSKUCodeTaken，实际 %v", err)
	}
	// 加工前缀也要能归一：`SZ_DRAWERSMOKE_002` 在 SZ 仓里应落成裸码。
	if got := createStock(t, f, wh.ID, "SZ_DRAWERSMOKE_002", 1, true); got.SKUCode != "DRAWERSMOKE_002" {
		t.Fatalf("仓码前缀应被剥成裸码，实际 %q", got.SKUCode)
	}
}

// TestReserveWritesNoMovement 验收 7 / 9：
// 预占只改 reserved_quantity、不写流水；并发预占不超卖。
func TestReserveWritesNoMovement(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Reserve")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 10, "purchase_in")

	before := f.movementCount(t)
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-1",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 3, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	if after := f.movementCount(t); after != before {
		t.Fatalf("预占不得写流水：之前 %d 条，之后 %d 条", before, after)
	}
	if got := f.stockReserved(t, v.ID, wh.ID); got != 3 {
		t.Fatalf("预占后 reserved 应为 3，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("预占不得改 quantity：期望 10，实际 %d", got)
	}

	// 并发：两 goroutine 各预占 6，行上 quantity = 10 → 恰一成一败，reserved = 6。
	// 先把预占清回 0（各自独立），再跑并发。
	if err := f.db.Exec("UPDATE inventory_stocks SET reserved_quantity = 0 "+
		"WHERE variant_id = ? AND warehouse_id = ?", v.ID, wh.ID).Error; err != nil {
		t.Fatalf("重置预占失败: %v", err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
				ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-concurrent",
				Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 6, WarehouseID: wh.ID}},
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, failed := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else {
			failed++
		}
	}
	if ok != 1 || failed != 1 {
		t.Fatalf("并发预占应恰一成一败，实际成功 %d / 失败 %d", ok, failed)
	}
	if got := f.stockReserved(t, v.ID, wh.ID); got != 6 {
		t.Fatalf("并发后 reserved 应为 6（只成功一次），实际 %d", got)
	}
}

// TestReserveInsufficientRollsBackAllLines 验收 8：
// 两行预占请求（第一行够、第二行不够）→ 报错后第一行 reserved 回到原值。
func TestReserveInsufficientRollsBackAllLines(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p1 := mustProduct(t, f, "Enough")
	p2 := mustProduct(t, f, "Short")
	v1 := f.firstVariant(t, p1.ID)
	v2 := f.firstVariant(t, p2.ID)
	changeIn(t, f, p1, v1, wh.ID, 10, "purchase_in")
	changeIn(t, f, p2, v2, wh.ID, 2, "purchase_in")

	_, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-short",
		Lines: []inventorydto.ReserveStockLine{
			{VariantID: v1.ID, Quantity: 5, WarehouseID: wh.ID},
			{VariantID: v2.ID, Quantity: 9, WarehouseID: wh.ID}, // 只有 2 件
		},
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockInsufficient)) {
		t.Fatalf("第二行不足应报 ErrStockInsufficient，实际 %v", err)
	}
	if got := f.stockReserved(t, v1.ID, wh.ID); got != 0 {
		t.Fatalf("整单拒绝时第一行的预占必须回到原值 0，实际 %d", got)
	}
	if got := f.stockReserved(t, v2.ID, wh.ID); got != 0 {
		t.Fatalf("不足的那一行不该留下预占，实际 %d", got)
	}
}

// TestReleaseReservation 释放预占：只改 reserved，不改 quantity、不写流水；超释放打回。
func TestReleaseReservation(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Release")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 10, "purchase_in")
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-release",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 4, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	before := f.movementCount(t)
	if _, err := f.inventory.ReleaseStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-release",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 4, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("释放失败: %v", err)
	}
	if got := f.stockReserved(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("释放后 reserved 应为 0，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("释放不得改 quantity：期望 10，实际 %d", got)
	}
	if after := f.movementCount(t); after != before {
		t.Fatalf("释放不得写流水：之前 %d 条，之后 %d 条", before, after)
	}
	// 超释放：reserved 已经是 0，再释放 1 件必须打回（不静默夹到 0）。
	if _, err := f.inventory.ReleaseStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-release",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 1, WarehouseID: wh.ID}},
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrReserveUnderflow)) {
		t.Fatalf("超释放应报 ErrReserveUnderflow，实际 %v", err)
	}
}

// TestOrphanRowsStayOutOfVariantTotals 验收 10：
// 无归属行（没有任何变体引用它）不进商品侧聚合 —— 641 之后聚合的入口是
// 「变体引用了哪些货行」，不是 inventory_stocks.variant_id。
func TestOrphanRowsStayOutOfVariantTotals(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Totals")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	// 一条没有变体引用的 7 件货。
	orphan := createStock(t, f, wh.ID, "ORPHAN_007", 7, true)

	// 变体引用的货行 id 清单里只有它自己那条。
	refs, err := productmodel.NewModel(f.db).ListVariantsByIDs(ctx, []string{v.ID}, f.projectID)
	if err != nil || len(refs) != 1 {
		t.Fatalf("读变体失败: %v", err)
	}
	ids := []string{}
	for _, r := range productmodel.DecodeWarehouseSKUs(refs[0].WarehouseSKUs) {
		ids = append(ids, r.StockID)
	}
	if len(ids) != 1 {
		t.Fatalf("变体应只引用一条货行，实际 %v", ids)
	}
	if ids[0] == orphan.ID {
		t.Fatalf("无归属行不该被登记进任何变体的引用清单")
	}
	facts, err := inventorymodel.NewModel(f.db).ListStockFactsByIDs(ctx, f.projectID, ids)
	if err != nil {
		t.Fatalf("读货行失败: %v", err)
	}
	if len(facts) != 1 || facts[0].Quantity != 5 {
		t.Fatalf("已知变体只该看到自己那条 5 件，实际 %+v", facts)
	}
}

// TestDeleteWarehouseBlockedByOrphanStock 验收 11：
// 仓里只有一条无归属且有数量的行 → 删仓被拒（不因 variant_id 为空而漏判）。
func TestDeleteWarehouseBlockedByOrphanStock(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 先建一个默认仓（删仓守卫对默认仓另有拒绝，故被测的仓不是默认仓）。
	f.createWarehouse(t, "SZ", "苏州仓", true)
	wh := f.createWarehouse(t, "SH", "上海仓", false)
	createStock(t, f, wh.ID, "ONLY_ORPHAN", 3, true)

	if err := f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{
		ID: wh.ID, ProjectID: f.projectID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrWarehouseHasStock)) {
		t.Fatalf("仓里只有无归属存货时删仓应报 ErrWarehouseHasStock，实际 %v", err)
	}
}

// TestStockCreateRequiresTrackForQuantity 建档的自相矛盾输入被拒（数量 > 0 却不跟踪）。
func TestStockCreateRequiresTrackForQuantity(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	_, err := f.inventory.CreateStock(context.Background(), &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "CONTRADICT_001",
		Quantity: 3, TrackQuantity: false,
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockUntrackedQuantity)) {
		t.Fatalf("有数量却不跟踪应被拒为 ErrStockUntrackedQuantity，实际 %v", err)
	}
}

// TestStockAlertCounts 验收（W4 §10.5 ③）：库存预警四卡口径。
//
// 构造四行：quantity=0（跟踪）/ quantity=5 reserved=5 / quantity=5 reserved=0 /
// track_quantity=false → OutOfStock=1, LowStock=1, Alert=2, Total=3。
func TestStockAlertCounts(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)

	// ① quantity = 0（跟踪）—— 缺货。
	p1 := mustProduct(t, f, "预警缺货")
	v1 := f.firstVariant(t, p1.ID)
	changeIn(t, f, p1, v1, wh.ID, 1, "purchase_in")
	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 0 WHERE variant_id = ? AND warehouse_id = ?",
		v1.ID, wh.ID).Error; err != nil {
		t.Fatalf("把库存改成 0 失败: %v", err)
	}

	// ② quantity = 5、reserved = 5 —— 货在但全被占住（可售为 0）。
	p2 := mustProduct(t, f, "预警被占")
	v2 := f.firstVariant(t, p2.ID)
	changeIn(t, f, p2, v2, wh.ID, 5, "purchase_in")
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-alert",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v2.ID, Quantity: 5, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}

	// ③ quantity = 5、reserved = 0 —— 健康（不进预警）。
	p3 := mustProduct(t, f, "预警健康")
	v3 := f.firstVariant(t, p3.ID)
	changeIn(t, f, p3, v3, wh.ID, 5, "purchase_in")

	// ④ 跟踪关闭（无限）—— 永远「有货」，不进预警。
	loose := createStock(t, f, wh.ID, "ALERT_UNTRACKED", 0, false)
	if loose.TrackQuantity {
		t.Fatalf("未跟踪行不该是跟踪状态")
	}
	// 商品侧自动建的那些初始 0 行也是在跟踪状态 —— 它们会被算进 Total / OutOfStock，
	// 所以断言用「增量」而不是绝对值：本用例另外造了 4 行，其中 3 行是跟踪的。
	before, err := f.inventory.StockAlert(ctx, f.projectID)
	if err != nil {
		t.Fatalf("读库存预警失败: %v", err)
	}
	// 已建商品（p1 起）各有一行跟踪库存；p1 的那行是 0（缺货）、p2 的是可售 0（低库存）、
	// p3 的是健康。所以三个增量分别是 +1 / +1 / +0。
	if before.OutOfStock != 1 {
		t.Fatalf("缺货应为 1（p1 那一行 quantity=0），实际 %d", before.OutOfStock)
	}
	if before.LowStock != 1 {
		t.Fatalf("可售为 0 应为 1（p2 那一行 5/−5），实际 %d", before.LowStock)
	}
	if before.Alert != 2 {
		t.Fatalf("要处理应为 2（缺货 +1、可售为 0 +1），实际 %d", before.Alert)
	}
	if before.Total != 3 {
		t.Fatalf("跟踪数量的行应为 3（p1/p2/p3 各一行；未跟踪行不计），实际 %d", before.Total)
	}
}
