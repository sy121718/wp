package productservice

import (
	"testing"

	inventoryenums "go_wp/internal/module/inventory/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"go_wp/pkg/facing"
)

func errKey(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("期望一条业务错误，实际 nil")
	}
	fact, ok := facing.FactOf(err)
	if !ok {
		t.Fatalf("期望业务错误事实，实际 %v", err)
	}
	return fact.Key
}

func stockRow(warehouseID, sku string, qty int) productdto.VariantStockRowResp {
	return productdto.VariantStockRowResp{WarehouseID: warehouseID, SKUCode: sku, Quantity: qty}
}

func TestStockRowKeyUsesWarehouseAndSKU(t *testing.T) {
	a := productdto.VariantStockRowResp{WarehouseID: "w1", StockID: "s1", SKUCode: "SZ_001"}
	b := productdto.VariantStockRowResp{WarehouseID: "w1", StockID: "s2", SKUCode: "SZ_001"}
	if stockRowKey(a) != stockRowKey(b) {
		t.Fatalf("同一仓同一 SKU 的不同货行 id 必须落同一个判重键：%q vs %q", stockRowKey(a), stockRowKey(b))
	}
	if got := stockRowKey(stockRow("w2", "SZ_001", 0)); got == stockRowKey(a) {
		t.Fatalf("不同仓不能共用判重键：%q", got)
	}
}

func TestStockRowKeyEmpty(t *testing.T) {
	for _, r := range []productdto.VariantStockRowResp{
		{SKUCode: "SZ_001"},
		{WarehouseID: "w1"},
		{WarehouseID: "  ", SKUCode: "  "},
	} {
		if key := stockRowKey(r); key != "" {
			t.Fatalf("%+v 不该有判重键，实际 %q", r, key)
		}
	}
}

func TestPlanVariantStockApplyRejectsDuplicateInVariant(t *testing.T) {
	_, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v1": {stockRow("w1", "SZ_001", 5), stockRow("w1", "SZ_001", 8)},
	}, map[string]bool{"v1": true})
	if got := errKey(t, err); got != productenums.ErrVariantStockDuplicated {
		t.Fatalf("期望 %s，实际 %s", productenums.ErrVariantStockDuplicated, got)
	}
}

func TestPlanVariantStockApplyAllowsSharedRowWithSameQuantity(t *testing.T) {
	plan, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v2": {stockRow("w1", "SZ_001", 5)},
		"v1": {stockRow("w1", "SZ_001", 5)},
	}, map[string]bool{"v1": true, "v2": true})
	if err != nil {
		t.Fatalf("数量一致时不该拒绝：%v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("两个变体各一条计划，实际 %d", len(plan))
	}

	if plan[0].VariantID != "v1" || len(plan[0].Rows) != 1 {
		t.Fatalf("排序第一的变体应保留这条行：%+v", plan[0])
	}
	if plan[1].VariantID != "v2" || len(plan[1].Rows) != 0 {
		t.Fatalf("后一个变体的重复行应被去重：%+v", plan[1])
	}
}

func TestPlanVariantStockApplyRejectsConflictingQuantity(t *testing.T) {
	_, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v1": {stockRow("w1", "SZ_001", 5)},
		"v2": {stockRow("w1", "SZ_001", 9)},
	}, map[string]bool{})
	if got := errKey(t, err); got != productenums.ErrVariantStockDuplicated {
		t.Fatalf("期望 %s，实际 %s", productenums.ErrVariantStockDuplicated, got)
	}
}

func TestPlanVariantStockApplyRejectsNegative(t *testing.T) {
	_, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v1": {stockRow("w1", "SZ_001", -1)},
	}, map[string]bool{})
	if got := errKey(t, err); got != inventoryenums.ErrStockQuantityInvalid {
		t.Fatalf("期望 %s，实际 %s", inventoryenums.ErrStockQuantityInvalid, got)
	}
}

func TestPlanVariantStockApplySortsVariants(t *testing.T) {
	plan, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v3": {stockRow("w1", "C", 1)},
		"v1": {stockRow("w1", "A", 1)},
		"v2": {stockRow("w1", "B", 1)},
	}, map[string]bool{"v1": true})
	if err != nil {
		t.Fatalf("不该失败：%v", err)
	}
	want := []string{"v1", "v2", "v3"}
	for i, id := range want {
		if plan[i].VariantID != id {
			t.Fatalf("第 %d 个计划应是 %s，实际 %s", i, id, plan[i].VariantID)
		}
	}
	if !plan[0].Prune {
		t.Fatal("Prune 标记要跟着 prune map 走（v1 在里头）")
	}
	if plan[1].Prune {
		t.Fatal("v2 不在 prune map 里，不该裁它的引用")
	}
}

func TestPlanVariantStockApplyEmpty(t *testing.T) {
	plan, err := planVariantStockApply(nil, nil)
	if err != nil || plan != nil {
		t.Fatalf("空输入应得 (nil, nil)，实际 (%v, %v)", plan, err)
	}
}

func TestGroupVariantStockRowsSkipsDangling(t *testing.T) {
	groups := groupVariantStockRows(nil, nil)
	if len(groups) != 0 {
		t.Fatalf("没有引用时不该有分组：%+v", groups)
	}
}
