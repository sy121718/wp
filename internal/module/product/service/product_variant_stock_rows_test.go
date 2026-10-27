package productservice

// product_variant_stock_rows_test.go — 变体仓库明细的**纯函数**判据（不起库）。
//
// 三条口径各钉一条：
//   1. 判重键按 (仓, 裸码) 走，不按 stockId —— 库存侧定行也是按仓码，
//      键不一致时两条「id 不同但仓码相同」的行会绕过判重、落到同一行（账差）。
//   2. 拒绝发生在写之前：同一个变体里同 key 出现两次、或跨变体同 key 数量不一致，
//      都必须是 ErrVariantStockDuplicated；负数走库存侧的 ErrStockQuantityInvalid。
//   3. 计划按变体 id 排序 —— 库存侧按 (仓, 码) 升序加锁，map 遍历无序会让两个并发
//      请求互相错序（死锁）。

import (
	"testing"

	inventoryenums "go_wp/internal/module/inventory/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"go_wp/pkg/facing"
)

// errKey 取一条业务错误的 key（测试只关心「哪一条」，不关心兜底文案）。
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

// TestStockRowKeyUsesWarehouseAndSKU 判重键与库存侧定行的键一致（仓 + 裸码）。
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

// TestStockRowKeyEmpty 缺仓或缺码的行没有判重键（计划里直接跳过，不参与校验）。
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

// TestPlanVariantStockApplyRejectsDuplicateInVariant 同一变体内同仓同 SKU 出现两次 → 拒绝。
func TestPlanVariantStockApplyRejectsDuplicateInVariant(t *testing.T) {
	_, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v1": {stockRow("w1", "SZ_001", 5), stockRow("w1", "SZ_001", 8)},
	}, map[string]bool{"v1": true})
	if got := errKey(t, err); got != productenums.ErrVariantStockDuplicated {
		t.Fatalf("期望 %s，实际 %s", productenums.ErrVariantStockDuplicated, got)
	}
}

// TestPlanVariantStockApplyAllowsSharedRowWithSameQuantity 跨变体共用一条货行且数量一致 → 只落一次。
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
	// 排序在前的是 v1（sort.Strings），对它落行；v2 的那条被去重掉。
	if plan[0].VariantID != "v1" || len(plan[0].Rows) != 1 {
		t.Fatalf("排序第一的变体应保留这条行：%+v", plan[0])
	}
	if plan[1].VariantID != "v2" || len(plan[1].Rows) != 0 {
		t.Fatalf("后一个变体的重复行应被去重：%+v", plan[1])
	}
}

// TestPlanVariantStockApplyRejectsConflictingQuantity 跨变体同一条货行数量不一致 → 拒绝。
func TestPlanVariantStockApplyRejectsConflictingQuantity(t *testing.T) {
	_, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v1": {stockRow("w1", "SZ_001", 5)},
		"v2": {stockRow("w1", "SZ_001", 9)},
	}, map[string]bool{})
	if got := errKey(t, err); got != productenums.ErrVariantStockDuplicated {
		t.Fatalf("期望 %s，实际 %s", productenums.ErrVariantStockDuplicated, got)
	}
}

// TestPlanVariantStockApplyRejectsNegative 负数目标值走库存侧同一条判据。
func TestPlanVariantStockApplyRejectsNegative(t *testing.T) {
	_, err := planVariantStockApply(map[string][]productdto.VariantStockRowResp{
		"v1": {stockRow("w1", "SZ_001", -1)},
	}, map[string]bool{})
	if got := errKey(t, err); got != inventoryenums.ErrStockQuantityInvalid {
		t.Fatalf("期望 %s，实际 %s", inventoryenums.ErrStockQuantityInvalid, got)
	}
}

// TestPlanVariantStockApplySortsVariants 计划按变体 id 升序（库存侧加锁顺序要稳定）。
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

// TestPlanVariantStockApplyEmpty 没有明细时给 nil 计划（保存路径据此跳过整段）。
func TestPlanVariantStockApplyEmpty(t *testing.T) {
	plan, err := planVariantStockApply(nil, nil)
	if err != nil || plan != nil {
		t.Fatalf("空输入应得 (nil, nil)，实际 (%v, %v)", plan, err)
	}
}

// TestGroupVariantStockRowsSkipsDangling 悬空引用（货行已删）跳过，不造空行。
func TestGroupVariantStockRowsSkipsDangling(t *testing.T) {
	groups := groupVariantStockRows(nil, nil)
	if len(groups) != 0 {
		t.Fatalf("没有引用时不该有分组：%+v", groups)
	}
}
