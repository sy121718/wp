// product_variant_delete_guard_test.go — 变体删除守卫的两个引用面 + 容器 SKU 两处一致性。
//
// 两件事都是批次 C 的收口项：
//
//  1. 删除守卫 —— 变体删除前要拦住两个引用面：
//     库存非零 / **有过任何库存流水**（被订单用过）。
//     「保存变体清单」与单条删除**共用同一个守卫函数**
//     （两处各写一份必然分叉，表现为「接口能删掉、抽屉保存删不掉」）。
//
//  2. 容器 SKU 获取函数合并（docs/14 §1.2）—— 商品新建 / 变体生成两处
//     都经 buildProductContainerSKU 这一个入口。既有 SKU 一律不重算。
package feature

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// guardFixture 隔离 PG schema + 生产迁移/种子 + 真实工程、商品与库存三个 service。
type guardFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projectID string
	// sz 是默认仓（商品创建时的首个变体落在这里）；sh 是第二个仓（仓库来源的用例都在它上面）。
	sz *inventorydto.WarehouseResp
	sh *inventorydto.WarehouseResp
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "变体守卫测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)
	products.SetInventory(inventorymodel.NewModel(db))
	products.SetAvailabilityPort(inv)
	sz, err := inv.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SZ", Name: "苏州仓",
	})
	if err != nil {
		t.Fatalf("建默认仓失败: %v", err)
	}
	sh, err := inv.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SH", Name: "上海仓",
	})
	if err != nil {
		t.Fatalf("建第二个仓失败: %v", err)
	}
	return &guardFixture{
		inventory: inv, products: products, db: db,
		projectID: project.ID, sz: sz, sh: sh,
	}
}

// containerSKU 直读 products.sku_code（容器主体 SKU 的唯一落点，迁移 246）。
func (f *guardFixture) containerSKU(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

// mkAttr 建一个「参与变体」的属性组（值与 key 一一对应）。
func (f *guardFixture) mkAttr(t *testing.T, name, key string, valueKeys []string) *productdto.AttributeResp {
	t.Helper()
	values := make([]productdto.AttributeValueReq, 0, len(valueKeys))
	for _, k := range valueKeys {
		values = append(values, productdto.AttributeValueReq{Label: strings.ToUpper(k), Key: k})
	}
	attr, err := f.products.CreateAttribute(context.Background(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: name, Key: key, Values: values,
	})
	if err != nil {
		t.Fatalf("创建属性组 %s 失败: %v", name, err)
	}
	return attr
}

// mkProduct 建一个变体商品（可引用属性组；首个无规格变体由创建路径自动生成）。
func (f *guardFixture) mkProduct(t *testing.T, name, slug string, attributeIDs []string, price *float64) *productdto.ProductResp {
	t.Helper()
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		AttributeIDs: attributeIDs, DefaultPrice: price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return res
}

// variantsOf 读商品的全部变体。
func (f *guardFixture) variantsOf(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail.Variants
}

// generateAll 走「预览—保存」生成该商品的全部组合（与后台抽屉同一条路径）。
func (f *guardFixture) generateAll(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	ctx := context.Background()
	pv, err := f.products.PreviewVariantCombinations(ctx, &productdto.PreviewVariantReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
	})
	if err != nil {
		t.Fatalf("预览组合失败: %v", err)
	}
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	if _, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
		OperatorID: "tester", Rows: rows,
	}); err != nil {
		t.Fatalf("保存变体清单失败: %v", err)
	}
	return f.variantsOf(t, productID)
}

// seedWarehouseRow 在指定仓给某个变体登记一条仓库 SKU（inventory_stocks 的一行）。
func (f *guardFixture) seedWarehouseRow(t *testing.T, warehouseID, skuCode string, v *productdto.VariantResp) {
	t.Helper()
	if _, err := f.inventory.EnsureStock(context.Background(), &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID,
		ProductID: v.ProductID, VariantID: v.ID, SKUCode: skuCode,
	}); err != nil {
		t.Fatalf("登记仓库 SKU %s 失败: %v", skuCode, err)
	}
}

// changeStock 走真实库存变动链路（in / out），用于造「有过流水」的变体。
func (f *guardFixture) changeStock(t *testing.T, v *productdto.VariantResp, direction string, qty int) {
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
func (f *guardFixture) saveListRaw(t *testing.T, productID string, rows []productdto.VariantListResp) (*productdto.SaveVariantListResp, error) {
	t.Helper()
	return f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
		OperatorID: "tester", Rows: rows,
	})
}

// TestVariantDeleteGuardTwoReasons 两个守卫各一条用例：
// 库存非零 / 有过库存流水 —— 一条都不能删，且逐条回带原因；
// 没有任何引用面的那个变体照常删除。
func TestVariantDeleteGuardTwoReasons(t *testing.T) {
	f := newGuardFixture(t)
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
	withStock, movedOnly, unreferenced := variants[0], variants[1], variants[2]

	// ① 库存非零。
	f.changeStock(t, withStock, inventoryenums.DirectionIn, 3)
	// ② 有过库存流水但库存已回零（买入又卖出）：非零库存守卫拦不住它，必须靠流水守卫。
	f.changeStock(t, movedOnly, inventoryenums.DirectionIn, 2)
	f.changeStock(t, movedOnly, inventoryenums.DirectionOut, 2)

	// 清单里一个都不保留 → 四个变体全在删除范围内：两种原因各一条，其余两个应被真的删掉。
	res, err := f.saveListRaw(t, p.ID, []productdto.VariantListResp{})
	if err != nil {
		t.Fatalf("保存空清单应只在逐条跳过里体现（不整批失败），实际报错: %v", err)
	}
	if res.Deleted != 2 {
		t.Fatalf("只有无引用面的两个该被删，实际 deleted=%d", res.Deleted)
	}
	reasons := map[string]string{}
	for _, skip := range res.Skipped {
		reasons[skip.VariantID] = skip.Reason
	}
	want := map[string]string{
		withStock.ID: productenums.VariantSkipHasStock,
		movedOnly.ID: productenums.VariantSkipHasMovement,
	}
	for id, reason := range want {
		if reasons[id] != reason {
			t.Fatalf("变体 %s 应报 %s，实际 %+v", id, reason, res.Skipped)
		}
	}
	// 被拦下的变体必须还在库里（守卫是「不改状态」，不是「删了一半」）；
	// 没有引用面的必须真的没了（证明上面的 deleted=2 不是别处凑出来的）。
	left := f.variantsOf(t, p.ID)
	if len(left) != 2 {
		t.Fatalf("被拦下的两个变体必须留在库里，实际 %d 个", len(left))
	}
	for _, v := range left {
		if v.ID == unreferenced.ID {
			t.Fatalf("无引用面的变体 %s 应已被删除，实际仍在库里", unreferenced.ID)
		}
	}

	// 单条删除路径共用同一个守卫（同一批原因 key，不是另一套文案）。
	single := []struct {
		id     string
		reason string
	}{
		{withStock.ID, productenums.ErrVariantHasStock},
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

// TestContainerSKUTwoPathsConsistent 容器 SKU 的两处取得路径行为一致（本批合并为一个入口）：
//
//	① 商品新建（自己创建 / 从仓库选）—— 入口是 buildProductContainerSKU；
//	② 变体生成 —— 变体 SKU = <容器主体>_<属性值…>_V，主体段就是 products.sku_code。
//
// 两者共用同一个函数后，这两条断言就是「主体段只有一份」的可执行证明。
func TestContainerSKUTwoPathsConsistent(t *testing.T) {
	f := newGuardFixture(t)
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
