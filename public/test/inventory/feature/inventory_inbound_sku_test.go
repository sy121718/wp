package feature

// inventory_inbound_sku_test.go — 入库新建库存行时 sku_code 的口径不变量（真库）。
//
// 口径（docs/14 §1.1 与迁移 262）：**仓库里的 SKU 永远是裸码**（TEE），不带仓码前缀；
// 前缀只属于**商品侧**（SZ_TEE，标注归属 / 认领仓）。而入库不是商品侧的调用路径 ——
// RegisterReceipt / RegisterProductionInbound 在目标仓还没有该变体的行时，按**请求里的
// SKUCode** 建行，所以口径必须在本模块的入库入口自己收口：按目标仓短码幂等剥前缀 + 空串拒绝。
//
// 缺陷原本会现形的条件是**该变体一条库存行都没有**：只要有行，applyStockChangesTx 就会用
// 既有行的裸码覆盖入参（元数据兜底），问题被掩盖。所以下面每个场景都先把库存行删掉，
// 把库摆成「入库真的要新建一行」的状态 —— 那才是这条不变量真正被考验的地方。
//
// 断言一律**直查 inventory_stocks**：返回值是内存里的实体，只信它会把「归一写在内存里、
// 落库还是原值」这类错误放过（先例见 inventory_stock_invariant_test.go）。

import (
	"context"
	"testing"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	productservice "go_wp/internal/module/product/service"
)

// TestInboundCreatesStockRowWithBareWarehouseSKU 入库新建库存行时，
// sku_code 必须落成**仓库侧裸码**：带前缀的入参被幂等剥掉，空串则整条拒绝。
func TestInboundCreatesStockRowWithBareWarehouseSKU(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", inventoryenums.SourceTypeInternal)
	cost := 3.5

	// ① 生产入库：调用方给的是**商品侧带前缀**的编码（接口调用方的常态）。
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	bare := bareSKU(v.SKUCode, wh.Code)
	if bare == v.SKUCode {
		t.Fatalf("前置不成立：变体编码 %q 应带仓码前缀", v.SKUCode)
	}
	f.dropStockRows(t, v.ID)
	if _, err := f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 2, UnitCost: &cost, RequestID: "SKU-INV-PROD",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	if got := f.stockSKUCode(t, v.ID, wh.ID); got != bare {
		t.Fatalf("新建的库存行 sku_code 应为仓库侧裸码 %q，实际 %q", bare, got)
	}
	// 归一与商品侧剥前缀**同口径**（库存侧另有一份实现，这条断言就是它们的等价性背书；
	// 两边一旦分叉 —— 比如大小写 / 双前缀的处理不一致 —— 这里立刻红）。
	if want := productservice.StripWarehousePrefix(v.SKUCode, wh.Code); bare != want {
		t.Fatalf("库存侧归一 %q 与商品侧 StripWarehousePrefix %q 不一致", bare, want)
	}

	// ② 采购收货：采购行是**历史形态**（带前缀的商品侧编码）—— 本批之前页面就是这么落库的。
	p2 := mustProduct(t, f, "Mug")
	v2 := f.firstVariant(t, p2.ID)
	bare2 := bareSKU(v2.SKUCode, wh.Code)
	f.dropStockRows(t, v2.ID)
	order := mustPurchaseOrder(t, f, "PO-SKU-BARE", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p2, v2, 3, 5)})
	// 建单路径已归一；这里直接改回带前缀的形态，模拟存量行 / 外部直接写库的行。
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET sku_code = ? WHERE id = ?",
		v2.SKUCode, order.Lines[0].ID).Error; err != nil {
		t.Fatalf("构造带前缀的采购行失败: %v", err)
	}
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "SKU-INV-RECV")
	if got := f.stockSKUCode(t, v2.ID, wh.ID); got != bare2 {
		t.Fatalf("采购收货新建的库存行 sku_code 应为仓库侧裸码 %q，实际 %q", bare2, got)
	}
	// 同一个仓里的两条货各自有自己的裸码（修复前若都落空串，第二行会直接撞唯一约束）。
	if bare == bare2 {
		t.Fatalf("两个不同变体的裸码不应相同：%q", bare)
	}

	// ③ 空串一律拒绝：不再用空串建库存行（也就不会有「第二行撞 23505」）。
	p3 := mustProduct(t, f, "Chair")
	v3 := f.firstVariant(t, p3.ID)
	f.dropStockRows(t, v3.ID)
	_, err := f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p3.ID, VariantID: v3.ID, SKUCode: "   ",
		Quantity: 1, UnitCost: &cost, RequestID: "SKU-INV-EMPTY",
	})
	purchaseErr(t, err, inventoryenums.ErrStockSKURequired)
	if n := f.stockRowCount(t, v3.ID); n != 0 {
		t.Fatalf("空编码被拒绝时不应留下库存行，实际 %d 行", n)
	}
	var emptyRows int64
	if qerr := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ''", wh.ID).
		Scan(&emptyRows).Error; qerr != nil {
		t.Fatalf("统计空编码库存行失败: %v", qerr)
	}
	if emptyRows != 0 {
		t.Fatalf("本仓不应存在 sku_code 为空的库存行，实际 %d 行", emptyRows)
	}

	// ④ 唯一约束仍然生效：换一个变体、同一个裸码 → 必须被 (warehouse_id, sku_code) 拒绝。
	//    （它同时说明第 ③ 条拒绝的价值：空串若放行，第二条空编码的行就会撞在这里。）
	if ierr := f.db.Exec("INSERT INTO inventory_stocks "+
		"(project_id, warehouse_id, product_id, variant_id, sku_code) VALUES (?,?,?,?,?)",
		f.projectID, wh.ID, p3.ID, v3.ID, bare).Error; ierr == nil {
		t.Fatalf("同一仓内重复的 sku_code 应被 uq_inventory_stocks_warehouse_sku 拒绝")
	}
}

// TestPurchaseOrderLineRequiresWarehouseSKU 建采购行时缺仓库侧编码 → 明确拒绝，
// 而不是静默落一条空编码的行（那正是「页面表单没提交该字段」被放过的那条路）。
func TestPurchaseOrderLineRequiresWarehouseSKU(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	// 只给变体、不给编码：服务端按目标仓归一后为空 → 拒绝，且不留下半张单。
	_, err := f.inventory.CreatePurchaseOrder(context.Background(), &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-NO-SKU", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []inventorydto.PurchaseLineReq{{VariantID: v.ID, ProductID: p.ID, Quantity: 1, UnitPrice: 2}},
	})
	purchaseErr(t, err, inventoryenums.ErrStockSKURequired)
	if got := purchaseOrderCount(t, f); got != 0 {
		t.Fatalf("被拒绝的建单不应留下采购单，实际 %d 张", got)
	}
}

// —— 夹具小工具 ——

// dropStockRows 删掉某变体的全部库存行：把库摆成「入库确实要新建一行」的状态
// （有行时入库会沿用既有行的裸码，缺陷被掩盖）。
func (f *invFixture) dropStockRows(t *testing.T, variantID string) {
	t.Helper()
	if err := f.db.Exec("DELETE FROM inventory_stocks WHERE variant_id = ?", variantID).Error; err != nil {
		t.Fatalf("清空变体 %s 的库存行失败: %v", variantID, err)
	}
}

// stockSKUCode 直读某 (变体, 仓库) 库存行的 sku_code（不经 service，断言落库真值）。
func (f *invFixture) stockSKUCode(t *testing.T, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读库存行 sku_code 失败: %v", err)
	}
	return code
}

// stockRowCount 某变体的库存行数（断言「被拒绝时不留行」）。
func (f *invFixture) stockRowCount(t *testing.T, variantID string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", variantID).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}
