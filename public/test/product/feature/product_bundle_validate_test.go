// product_bundle_validate_test.go — 捆绑整单硬校验 / 库存约束 / 算价（issue #20 验收 4/5/6）。
package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// validateErr 调校验并断言被拒，返回错误文本。
func (f *bundleFixture) validateErr(t *testing.T, productID string, items ...productdto.BundleSelectItem) string {
	t.Helper()
	_, err := f.products.ValidateBundleSelection(context.Background(), &productdto.ValidateBundleSelectionReq{
		ProductID: productID, Items: items,
	})
	if err == nil {
		t.Fatalf("该选择应被后端拒绝，实际校验通过")
	}
	return err.Error()
}

// TestBundleSelectionBackendHardValidation 验收 4：
// 绕过前端直接调 service（等价于构造请求绕过前端）也必须被拒 —— 前端拦体验，后端拦正确性。
func TestBundleSelectionBackendHardValidation(t *testing.T) {
	f := newBundleFixture(t)
	if f == nil {
		return
	}
	mainPrice := 199.0
	main := f.mkProduct(t, "硬校验套餐", "strict-bundle", &mainPrice)
	addonA := f.mkProduct(t, "必选件", "strict-a", nil)
	addonB := f.mkProduct(t, "可选件", "strict-b", nil)
	va := f.firstVariant(t, addonA.ID)
	vb := f.firstVariant(t, addonB.ID)
	// 库存给足，避免先撞库存错误而掩盖区间 / 整单判定的结论。
	f.addStock(t, va, 50)
	f.addStock(t, vb, 50)

	cfg := productdto.NewEmptyBundleConfig()
	cfg.MinTotalQty, cfg.MaxTotalQty = 2, 5
	cfg.Options = []productdto.BundleOption{
		{VariantID: va.ID, Required: true, DefaultQty: 1, MinQty: 1, MaxQty: 2},
		{VariantID: vb.ID, Required: false, DefaultQty: 0, MinQty: 0, MaxQty: 0},
	}
	f.setBundleConfig(t, main.ID, cfg)

	// 漏填必选项 → 拒。
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: vb.ID, Qty: 1}); got != productenums.ErrBundleOptionRequired {
		t.Fatalf("漏必选应被拒（%s），实际 %q", productenums.ErrBundleOptionRequired, got)
	}
	// 单项超上限（3 > maxQty 2）→ 拒。
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: va.ID, Qty: 3}); got != productenums.ErrBundleQtyAboveMax {
		t.Fatalf("超单项上限应被拒（%s），实际 %q", productenums.ErrBundleQtyAboveMax, got)
	}
	// 低于整单最小总件数（1 < 2）→ 拒。
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: va.ID, Qty: 1}); got != productenums.ErrBundleTotalBelowMin {
		t.Fatalf("低于整单下限应被拒（%s），实际 %q", productenums.ErrBundleTotalBelowMin, got)
	}
	// 数量非法（负数）→ 拒。
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: va.ID, Qty: -1}); got != productenums.ErrBundleQtyInvalid {
		t.Fatalf("负数应被拒（%s），实际 %q", productenums.ErrBundleQtyInvalid, got)
	}
	// 配置之外的 SKU → 拒（不静默忽略）。
	stray := f.mkProduct(t, "无关商品", "stray-product", nil)
	vs := f.firstVariant(t, stray.ID)
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: vs.ID, Qty: 2}); got != productenums.ErrBundleVariantNotInConfig {
		t.Fatalf("配置外 SKU 应被拒（%s），实际 %q", productenums.ErrBundleVariantNotInConfig, got)
	}
	// 同一 SKU 重复提交 → 拒（数量必须一项一条）。
	dup := f.validateErr(t, main.ID,
		productdto.BundleSelectItem{VariantID: va.ID, Qty: 1},
		productdto.BundleSelectItem{VariantID: va.ID, Qty: 1})
	if dup != productenums.ErrBundleVariantDuplicated {
		t.Fatalf("重复 SKU 应被拒（%s），实际 %q", productenums.ErrBundleVariantDuplicated, dup)
	}
	// 合法的整单：必选 1 + 可选 1 = 2 件，正好达到下限。
	res, err := f.products.ValidateBundleSelection(context.Background(), &productdto.ValidateBundleSelectionReq{
		ProductID: main.ID,
		Items: []productdto.BundleSelectItem{
			{VariantID: va.ID, Qty: 1}, {VariantID: vb.ID, Qty: 1},
		},
	})
	if err != nil {
		t.Fatalf("合法选择不应被拒: %v", err)
	}
	if res.TotalQty != 2 || len(res.Items) != 2 {
		t.Fatalf("展开结果应为 2 项共 2 件，实际 %d 项 %d 件", len(res.Items), res.TotalQty)
	}
}

// TestBundleSelectionConstrainedByRealStock 验收 5：
// 数量上限受库存可用量约束，且可用量只读 inventory 真源 —— 缓存写得再大也拦得住。
func TestBundleSelectionConstrainedByRealStock(t *testing.T) {
	f := newBundleFixture(t)
	if f == nil {
		return
	}
	mainPrice := 88.0
	main := f.mkProduct(t, "库存约束套餐", "stock-bundle", &mainPrice)
	addon := f.mkProduct(t, "限量件", "limited-addon", nil)
	v := f.firstVariant(t, addon.ID)
	f.addStock(t, v, 3)

	cfg := bundleWith(v.ID, true, 1, 1, 0, 0, 0) // 最大与整单上限都不设，唯一约束就是库存
	f.setBundleConfig(t, main.ID, cfg)

	// 真源只有 3 件：选 4 件被拒。
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: v.ID, Qty: 4}); got != productenums.ErrBundleQtyAboveStock {
		t.Fatalf("超真源可用量应被拒（%s），实际 %q", productenums.ErrBundleQtyAboveStock, got)
	}

	// 把商品侧展示缓存改成 999（脏数据 / 并发滞后）：结论必须完全不变。
	if got := f.validateErr(t, main.ID, productdto.BundleSelectItem{VariantID: v.ID, Qty: 99}); got != productenums.ErrBundleQtyAboveStock {
		t.Fatalf("可用量必须只读真源，缓存 999 不得放行，实际 %q", got)
	}

	// 读取侧同样以真源为准：配置详情里的可用量是 3，不是缓存里的 999。
	detail, err := f.products.GetBundleConfig(context.Background(), &productdto.GetBundleConfigReq{ProductID: main.ID})
	if err != nil {
		t.Fatalf("读配置失败: %v", err)
	}
	if len(detail.Options) != 1 || detail.Options[0].Available != 3 {
		t.Fatalf("可用量应为真源的 3，实际 %+v", detail.Options)
	}

	// 真源补到 4 件后，选 4 件通过。
	f.addStock(t, v, 1)
	res, err := f.products.ValidateBundleSelection(context.Background(), &productdto.ValidateBundleSelectionReq{
		ProductID: main.ID, Items: []productdto.BundleSelectItem{{VariantID: v.ID, Qty: 4}},
	})
	if err != nil {
		t.Fatalf("真源足够时不应被拒: %v", err)
	}
	if res.TotalQty != 4 {
		t.Fatalf("总件数应为 4，实际 %d", res.TotalQty)
	}
}

// TestBundlePriceIsBasePriceCostKept 验收 6：
// 套餐价 = 主体自定价（与子项价格无关），子项成本仍在后台保留。
func TestBundlePriceIsBasePriceCostKept(t *testing.T) {
	f := newBundleFixture(t)
	if f == nil {
		return
	}
	mainPrice := 199.0
	main := f.mkProduct(t, "算价套餐", "price-bundle", &mainPrice)
	addon := f.mkProduct(t, "成本件", "cost-addon", nil)
	v := f.firstVariant(t, addon.ID)

	// 子项价格 9.9 / 成本 3.5（后台可见；前台不展示价格）。
	price99, cost35 := 9.9, 3.5
	if _, err := f.products.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: v.ID, Price: &price99, CostPrice: &cost35,
	}); err != nil {
		t.Fatalf("改变体价格/成本失败: %v", err)
	}
	f.addStock(t, v, 10)
	f.setBundleConfig(t, main.ID, bundleWith(v.ID, true, 2, 1, 0, 0, 0))

	res, err := f.products.ValidateBundleSelection(context.Background(), &productdto.ValidateBundleSelectionReq{
		ProductID: main.ID, Items: []productdto.BundleSelectItem{{VariantID: v.ID, Qty: 2}},
	})
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	// 套餐价 = 主体自定价 199：既不是子项价格之和（19.8），也不是两者相加（218.8）。
	if res.TotalPrice != 199 {
		t.Fatalf("套餐价应为主体自定价 199，实际 %v", res.TotalPrice)
	}
	// 子项成本 3.5 × 2 = 7.0 仍在后台口径里保留。
	if res.TotalCost < 6.99 || res.TotalCost > 7.01 {
		t.Fatalf("子项成本合计应为 7.00，实际 %v", res.TotalCost)
	}
	if len(res.Items) != 1 || res.Items[0].UnitPrice != 9.9 || res.Items[0].CostPrice == nil || *res.Items[0].CostPrice != 3.5 {
		t.Fatalf("展开行应带子项价格与成本快照: %+v", res.Items)
	}

	// 配置详情（后台口径）同样保留子项价格与成本。
	detail, derr := f.products.GetBundleConfig(context.Background(), &productdto.GetBundleConfigReq{ProductID: main.ID})
	if derr != nil {
		t.Fatalf("读配置失败: %v", derr)
	}
	if detail.BasePrice != 199 {
		t.Fatalf("主体自定价应为 199，实际 %v", detail.BasePrice)
	}
	if detail.Options[0].ItemPrice != 9.9 || detail.Options[0].CostPrice == nil || *detail.Options[0].CostPrice != 3.5 {
		t.Fatalf("后台应保留子项价格与成本: %+v", detail.Options[0])
	}
}
