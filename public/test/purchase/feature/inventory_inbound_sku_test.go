package feature

import (
	"context"
	"testing"

	inventoryenums "go_wp/internal/module/inventory/enums"
	productservice "go_wp/internal/module/product/service"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

func TestInboundCreatesStockRowWithBareWarehouseSKU(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", purchaseenums.SourceTypeInternal)
	cost := 3.5

	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	bare := bareSKU(v.SKUCode, wh.Code)
	if bare == v.SKUCode {
		t.Fatalf("前置不成立：变体编码 %q 应带仓码前缀", v.SKUCode)
	}
	f.dropStockRows(t, v.ID)
	if _, err := f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 2, UnitCost: &cost, RequestID: "SKU-INV-PROD",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	if got := f.stockSKUCode(t, v.ID, wh.ID); got != bare {
		t.Fatalf("新建的库存行 sku_code 应为仓库侧裸码 %q，实际 %q", bare, got)
	}

	if want := productservice.StripWarehousePrefix(v.SKUCode, wh.Code); bare != want {
		t.Fatalf("库存侧归一 %q 与商品侧 StripWarehousePrefix %q 不一致", bare, want)
	}

	p2 := mustProduct(t, f, "Mug")
	v2 := f.firstVariant(t, p2.ID)
	bare2 := bareSKU(v2.SKUCode, wh.Code)
	f.dropStockRows(t, v2.ID)
	order := mustPurchaseOrder(t, f, "PO-SKU-BARE", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p2, v2, 3, 5)})

	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET sku_code = ? WHERE id = ?",
		v2.SKUCode, order.Lines[0].ID).Error; err != nil {
		t.Fatalf("构造带前缀的采购行失败: %v", err)
	}
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "SKU-INV-RECV")
	if got := f.stockSKUCode(t, v2.ID, wh.ID); got != bare2 {
		t.Fatalf("采购收货新建的库存行 sku_code 应为仓库侧裸码 %q，实际 %q", bare2, got)
	}

	if bare == bare2 {
		t.Fatalf("两个不同变体的裸码不应相同：%q", bare)
	}

	p3 := mustProduct(t, f, "Chair")
	v3 := f.firstVariant(t, p3.ID)
	f.dropStockRows(t, v3.ID)
	_, err := f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
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

	if ierr := f.db.Exec("INSERT INTO inventory_stocks "+
		"(project_id, warehouse_id, product_id, variant_id, sku_code) VALUES (?,?,?,?,?)",
		f.projectID, wh.ID, p3.ID, v3.ID, bare).Error; ierr == nil {
		t.Fatalf("同一仓内重复的 sku_code 应被 uq_inventory_stocks_warehouse_sku 拒绝")
	}
}

func TestPurchaseOrderLineRequiresWarehouseSKU(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)

	_, err := f.purchase.CreatePurchaseOrder(context.Background(), &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-NO-SKU", SourceID: src.ID, WarehouseID: wh.ID,
		Lines: []purchasedto.PurchaseLineReq{{VariantID: v.ID, ProductID: p.ID, Quantity: 1, UnitPrice: 2}},
	})
	purchaseErr(t, err, inventoryenums.ErrStockSKURequired)
	if got := purchaseOrderCount(t, f); got != 0 {
		t.Fatalf("被拒绝的建单不应留下采购单，实际 %d 张", got)
	}
}

func (f *purchaseFixture) dropStockRows(t *testing.T, variantID string) {
	t.Helper()
	if err := f.db.Exec("DELETE FROM inventory_stocks WHERE variant_id = ?", variantID).Error; err != nil {
		t.Fatalf("清空变体 %s 的库存行失败: %v", variantID, err)
	}
}

func (f *purchaseFixture) stockSKUCode(t *testing.T, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读库存行 sku_code 失败: %v", err)
	}
	return code
}

func (f *purchaseFixture) stockRowCount(t *testing.T, variantID string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", variantID).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}
