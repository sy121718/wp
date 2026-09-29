// product_variant_delete_guard_test.go — 变体删除守卫的四个引用面 + 容器 SKU 三处一致性。
//
// 两件事都是批次 C 的收口项：
//
//  1. 删除守卫（docs/14 §8 的「待补」两项）—— 变体删除前要拦住四个引用面：
//     库存非零 / BOM 引用 / **捆绑成员引用** / **有过任何库存流水**（被订单用过）。
//     前两个是既有实现，后两个本批补上；「保存变体清单」与单条删除**共用同一个守卫函数**
//     （两处各写一份必然分叉，表现为「接口能删掉、抽屉保存删不掉」）。
//
//  2. 容器 SKU 获取函数合并（docs/14 §1.2）—— 商品新建 / 变体生成 / 捆绑主体三处
//     都经 buildProductContainerSKU 这一个入口（行为差异只有一处且是刻意的：
//     捆绑不存在于仓库，永远不接仓码前缀）。既有 SKU 一律不重算。
package feature

import (
	"context"
	"strings"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// containerSKU 直读 products.sku_code（容器主体 SKU 的唯一落点，迁移 246）。
func (f *memberSrcFixture) containerSKU(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

// changeStock 走真实库存变动链路（in / out），用于造「有过流水」的变体。
func (f *memberSrcFixture) changeStock(t *testing.T, v *productdto.VariantResp, direction string, qty int) {
	t.Helper()
	reason := "purchase_in"
	if direction == inventoryenums.DirectionOut {
		reason = "sale_out"
	}
	if _, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, WarehouseID: f.sz.ID,
		Direction: direction, ReasonCode: reason, OperatorID: "tester",
		Lines: []inventorydto.StockChangeLineReq{{
			WarehouseID: f.sz.ID, ProductID: v.ProductID,
			VariantID: v.ID, SKUCode: v.SKUCode, Quantity: qty,
		}},
	}); err != nil {
		t.Fatalf("库存变动 %s %d 件失败: %v", direction, qty, err)
	}
}

// saveListRaw 以清单为准保存变体（返回结论，不改判失败）。
func (f *memberSrcFixture) saveListRaw(t *testing.T, productID string, rows []productdto.VariantListRow) (*productdto.SaveVariantListResp, error) {
	t.Helper()
	return f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
		OperatorID: "tester", Rows: rows,
	})
}

// TestVariantDeleteGuardFourReasons 四个守卫各一条用例：
// 库存非零 / BOM 引用 / 捆绑成员引用 / 有过库存流水 —— 一条都不能删，且逐条回带原因。
func TestVariantDeleteGuardFourReasons(t *testing.T) {
	f := newMemberSrcFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	price := 12.0
	p := f.mkProduct(t, "守卫商品", "delete-guard-product", []string{color.ID, size.ID}, &price)
	variants := f.generateAll(t, p.ID)
	if len(variants) != 4 {
		t.Fatalf("应有 4 个规格变体，实际 %d", len(variants))
	}
	withStock, bomComponent, bundleMember, movedOnly := variants[0], variants[1], variants[2], variants[3]

	// ① 库存非零。
	f.changeStock(t, withStock, inventoryenums.DirectionIn, 3)
	// ② BOM 引用：bomComponent 是某个父 SKU 的组件。
	if _, err := f.inventory.SetBOM(context.Background(), &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: withStock.ID, ParentSKUCode: withStock.SKUCode,
		Items: []inventorydto.BOMItemReq{{
			ComponentVariantID: bomComponent.ID, ComponentSKUCode: bomComponent.SKUCode, Quantity: 1,
		}},
	}); err != nil {
		t.Fatalf("配置 BOM 失败: %v", err)
	}
	// ③ 捆绑成员引用：某个捆绑容器的成员清单指向 bundleMember。
	bundle := f.mkBundle(t, "引用套餐", "delete-guard-bundle", "GUARD_REF_B", 45.0)
	cfg := productdto.NewEmptyBundleConfig()
	cfg.Options = []productdto.BundleOption{{
		VariantID: bundleMember.ID, Required: true, DefaultQty: 1, MinQty: 1,
	}}
	f.saveConfig(t, bundle.ID, cfg)
	// ④ 有过库存流水但库存已回零（买入又卖出）：非零库存守卫拦不住它，必须靠流水守卫。
	f.changeStock(t, movedOnly, inventoryenums.DirectionIn, 2)
	f.changeStock(t, movedOnly, inventoryenums.DirectionOut, 2)

	// 清单里一个都不保留 → 四个变体全在删除范围内，四种原因各一条。
	res, err := f.saveListRaw(t, p.ID, []productdto.VariantListRow{})
	if err != nil {
		t.Fatalf("保存空清单应只在逐条跳过里体现（不整批失败），实际报错: %v", err)
	}
	if res.Deleted != 0 {
		t.Fatalf("四个变体都有引用面，一个都不该被删，实际 deleted=%d", res.Deleted)
	}
	reasons := map[string]string{}
	for _, skip := range res.Skipped {
		reasons[skip.VariantID] = skip.Reason
	}
	want := map[string]string{
		withStock.ID:    productenums.VariantSkipHasStock,
		bomComponent.ID: productenums.VariantSkipReferenced,
		bundleMember.ID: productenums.VariantSkipBundleReferenced,
		movedOnly.ID:    productenums.VariantSkipHasMovement,
	}
	for id, reason := range want {
		if reasons[id] != reason {
			t.Fatalf("变体 %s 应报 %s，实际 %+v", id, reason, res.Skipped)
		}
	}
	// 被拦下的变体必须还在库里（守卫是「不改状态」，不是「删了一半」）。
	if len(f.variantsOf(t, p.ID)) != 4 {
		t.Fatalf("被拦下的变体必须留在库里，实际 %d 个", len(f.variantsOf(t, p.ID)))
	}

	// 单条删除路径共用同一个守卫（同一批原因 key，不是另一套文案）。
	single := []struct {
		id     string
		reason string
	}{
		{withStock.ID, productenums.ErrVariantHasStock},
		{bomComponent.ID, productenums.VariantSkipReferenced},
		{bundleMember.ID, productenums.VariantSkipBundleReferenced},
		{movedOnly.ID, productenums.VariantSkipHasMovement},
	}
	for _, tc := range single {
		err := f.products.DeleteVariant(context.Background(), &productdto.DeleteVariantReq{
			ID: tc.id, ProjectID: f.projectID,
		})
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("单条删除 %s 应报 %s，实际 %v", tc.id, tc.reason, err)
		}
	}
}

// TestContainerSKUThreePathsConsistent 容器 SKU 的三处取得路径行为一致（本批合并为一个入口）：
//
//	① 商品新建（自己创建 / 从仓库选）—— 入口是 buildProductContainerSKU；
//	② 变体生成 —— 变体 SKU = <容器主体>_<属性值…>_V，主体段就是 products.sku_code；
//	③ 捆绑主体 —— 自定义且恒以 _B 结尾，**不接仓码**（捆绑不存在于仓库）。
//
// 三者共用同一个函数后，这三条断言就是「主体段只有一份」的可执行证明。
func TestContainerSKUThreePathsConsistent(t *testing.T) {
	f := newMemberSrcFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 10.0

	// ①a 自己创建 + 选了仓：编码原样 + 仓码前缀。
	self := f.mkProduct(t, "自建商品", "sku-consist-self", nil, &price)
	// mkProduct 不带 sku，这里再建一个显式填编码的：改走 Create 原始入参。
	named := "自建编码商品"
	made, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: named, Slug: "sku-consist-custom",
		SKUCode: "MY-TEE", DefaultPrice: &price, WarehouseID: f.sz.ID,
	})
	if err != nil {
		t.Fatalf("自建商品（显式编码）失败: %v", err)
	}
	if got := f.containerSKU(t, made.ID); got != "SZ_MY-TEE" {
		t.Fatalf("自建 + 选仓的主体 SKU 应为 SZ_MY-TEE，实际 %q", got)
	}
	if v := f.variantsOf(t, made.ID); len(v) != 1 || v[0].SKUCode != "SZ_MY-TEE" {
		t.Fatalf("首个变体应等于容器主体，实际 %+v", v)
	}

	// ①b 从仓库选：主体 = <仓短码>_<仓库那条 SKU>，前端提交的 sku 不作数。
	// 用第二个仓（sh）登记一条独立的仓库 SKU：同一个仓里「我们自己的 SKU 仓内唯一」，
	// 在默认仓里复用它自己的编码会直接撞 UNIQUE (warehouse_id, sku_code)。
	host := f.mkProduct(t, "仓库货宿主", "sku-consist-host", nil, &price)
	hostVariant := f.variantsOf(t, host.ID)[0]
	f.seedWarehouseRow(t, f.sh.ID, "CONSIST-001", hostVariant)
	picked, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "从仓库选的商品", Slug: "sku-consist-picked",
		DefaultPrice: &price, SKUSource: "warehouse", WarehouseID: f.sh.ID,
		WarehouseSKU: "CONSIST-001", SKUCode: "FRONTEND-LIE",
	})
	if err != nil {
		t.Fatalf("从仓库选建商品失败: %v", err)
	}
	wantPicked := "SH_CONSIST-001"
	if got := f.containerSKU(t, picked.ID); got != wantPicked {
		t.Fatalf("从仓库选的主体 SKU 应为 %q，实际 %q", wantPicked, got)
	}
	if v := f.variantsOf(t, picked.ID); len(v) != 1 || v[0].SKUCode != wantPicked {
		t.Fatalf("首个变体应等于容器主体，实际 %+v", v)
	}

	// ② 变体生成：新变体的 SKU 以容器主体为前缀、以 _V 结尾。
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	gen := f.mkProduct(t, "组合商品", "sku-consist-generate", []string{color.ID}, &price)
	container := f.containerSKU(t, gen.ID)
	if container == "" {
		t.Fatalf("新建商品必须有容器主体 SKU")
	}
	generated := f.generateAll(t, gen.ID)
	if len(generated) != 2 {
		t.Fatalf("应生成 2 个组合变体，实际 %d", len(generated))
	}
	for _, v := range generated {
		if !strings.HasPrefix(v.SKUCode, container+"_") || !strings.HasSuffix(v.SKUCode, "_V") {
			t.Fatalf("变体 SKU 应为 <%s>_<属性值…>_V，实际 %q", container, v.SKUCode)
		}
	}

	// ③ 捆绑主体：自定义 + 恒以 _B 结尾；**不接仓码**（捆绑不存在于仓库）。
	custom := "GIFT"
	withWarehouse := f.mkBundle(t, "捆绑一", "sku-consist-bundle-1", custom, 20.0)
	if got := f.containerSKU(t, withWarehouse.ID); got != "GIFT_B" {
		t.Fatalf("捆绑主体应为 GIFT_B，实际 %q", got)
	}
	trailing := f.mkBundle(t, "捆绑二", "sku-consist-bundle-2", "gift_b", 20.0)
	if got := f.containerSKU(t, trailing.ID); got != "gift_b" {
		t.Fatalf("已带 _B 后缀（大小写不敏感）应原样保留，实际 %q", got)
	}
	// 缺后缀补齐 + 留空拒绝：留空不再静默派生（ErrBundleSKURequired）。
	suffixMissing := f.mkBundle(t, "捆绑三", "sku-consist-bundle-3", "WRAP", 20.0)
	if got := f.containerSKU(t, suffixMissing.ID); got != "WRAP_B" {
		t.Fatalf("缺后缀应补齐为 WRAP_B，实际 %q", got)
	}
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "无编码捆绑", Slug: "sku-consist-bundle-4",
		Type: productmodel.TypeBundle, SKUCode: "   ", DefaultPrice: &price,
	}); err == nil || !strings.Contains(err.Error(), productenums.ErrBundleSKURequired) {
		t.Fatalf("捆绑留空应报 %s，实际 %v", productenums.ErrBundleSKURequired, err)
	}

	// 存量不重算：改 URL 段 / 再存一次都不会动已有主体编码与变体编码。
	newSlug := "sku-consist-renamed"
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{
		ID: made.ID, ProjectID: f.projectID, Slug: &newSlug,
	}); err != nil {
		t.Fatalf("改商品 URL 段失败: %v", err)
	}
	if got := f.containerSKU(t, made.ID); got != "SZ_MY-TEE" {
		t.Fatalf("存量主体 SKU 不得被重算，实际 %q", got)
	}
	if got := f.variantsOf(t, made.ID)[0].SKUCode; got != "SZ_MY-TEE" {
		t.Fatalf("存量变体 SKU 不得被重算，实际 %q", got)
	}
	if got := f.containerSKU(t, self.ID); got == "" {
		t.Fatalf("用商品名派生不出 ASCII 段时应已明确失败，而不是落一个空主体编码")
	}
}
