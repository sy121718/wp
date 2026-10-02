// inventory_restock_untracked_test.go — 系统自动归还不得把「无限」行切成跟踪（BIZ-03）。
//
// 缺陷：入库 / 调整路径对**不跟踪**的行会 `track_quantity = true` 并把数量写回 ——
// 这条规则的本意是「人类显式给了数量就说明要开始跟踪」，但它**不区分人类意图与
// 系统自动归还**：取消订单归还与退货入库也走同一条函数并显式传数量。
//
// 触发序列：新建变体默认无限 → 下单 5 件（出库跳过，无流水）→ 取消订单 →
// 该行变成 track=true, quantity=5 → 商品从无限变有限，第 6 件直接库存不足。
//
// 判据（本文件）：
//  1. 归还对不跟踪行**跳过**（与出库对称）：不切换开关、不写数量、不写流水；
//  2. 归还对跟踪行照旧增加（不回退）；
//  3. 人类显式调整（管理员入库 / 盘点）仍然把无限行切成跟踪（设计意图保留）。
//
// 调用链用**订单侧适配器**（orderstock.Operator）而不是库存 service 直调：
// 要证明的正是「订单归还这条路不再切跟踪」，直调 service 绕过了适配层就证明不了。
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

// restockViaOrderTx 经订单侧库存契约（事务透传版）把货加回库存 —— 取消订单 / 退货入库
// 走的就是这一条。sourceType 用真实取值："order"（取消归还）/ "order_return"（退货入库）。
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

// TestRestockSkipsUntrackedRow 判据 1：系统自动归还对无限行跳过（不切跟踪 / 不写数量 / 无流水）。
func TestRestockSkipsUntrackedRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "无限商品归还")
	v := f.firstVariant(t, p.ID)

	// 前置：新建行默认无限。
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("前置条件不成立：新建行应默认不跟踪")
	}

	// 卖 5 件（无限行跳过，无流水），再取消订单归还 5 件。
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

	// 归还之后再卖 6 件仍不得被判库存不足（这是这条缺陷真正的用户可见后果）。
	if err := changeOut(f, p, v, wh.ID, 6, "SO-RESTOCK-OUT-2"); err != nil {
		t.Fatalf("归还之后无限行仍应可卖（第 6 件被判库存不足即缺陷复现）：%v", err)
	}
}

// TestRestockStillIncreasesTrackedRow 判据 2：跟踪行归还照旧增加（不回退）。
func TestRestockStillIncreasesTrackedRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "跟踪商品归还")
	v := f.firstVariant(t, p.ID)

	// 先入库 10 把这行切成跟踪。
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

	// 归还 4 件：跟踪行必须照常增加（这条路径是正常的退单流程，不能被「跳过」波及）。
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

// TestRestockMixedBatchOnlySkipsUntracked 判据 1 的批量形态：同一批里无限行跳过、跟踪行照加。
//
// 策略是**逐行**的：按批整体放行 / 整体拒绝都会在混合批里出错
// （真实场景：一单里既有跟踪商品也有无限商品）。
func TestRestockMixedBatchOnlySkipsUntracked(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	// 两个商品各一个变体：一个保持无限，一个显式入库 10 切成跟踪。
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

// TestExplicitChangeStillSwitchesUntrackedRow 判据 3：人类显式变动仍把无限行切成跟踪。
//
// 这条是**防回退**：修法是给「系统自动归还」加一条策略，不是取消「显式给数量就跟踪」
// 这条设计意图（迁移 261/262/263 拍板的口径）。
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
