package feature

import (
	"context"
	"errors"
	"go_wp/pkg/facing"
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

type guardFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projectID string

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

func (f *guardFixture) containerSKU(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

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

func (f *guardFixture) variantsOf(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail.Variants
}

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

func (f *guardFixture) seedWarehouseRow(t *testing.T, warehouseID, skuCode string, v *productdto.VariantResp) {
	t.Helper()
	if _, err := f.inventory.EnsureStock(context.Background(), &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID,
		ProductID: v.ProductID, VariantID: v.ID, SKUCode: skuCode,
	}); err != nil {
		t.Fatalf("登记仓库 SKU %s 失败: %v", skuCode, err)
	}
}

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

func (f *guardFixture) saveListRaw(t *testing.T, productID string, rows []productdto.VariantListResp) (*productdto.SaveVariantListResp, error) {
	t.Helper()
	return f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
		OperatorID: "tester", Rows: rows,
	})
}

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

	f.changeStock(t, withStock, inventoryenums.DirectionIn, 3)

	f.changeStock(t, movedOnly, inventoryenums.DirectionIn, 2)
	f.changeStock(t, movedOnly, inventoryenums.DirectionOut, 2)

	_, err := f.saveListRaw(t, p.ID, []productdto.VariantListResp{})
	var rejected facing.BlockedError
	if !errors.As(err, &rejected) {
		t.Fatalf("应整批拒绝：%v", err)
	}
	reasons := map[string]string{}
	for _, item := range rejected.BlockedItems() {
		reasons[item.ID] = item.ReasonKey
	}
	for id, reason := range map[string]string{withStock.ID: productenums.VariantSkipHasStock, movedOnly.ID: productenums.VariantSkipHasMovement} {
		if reasons[id] != reason {
			t.Fatalf("拒绝原因不符：%v", reasons)
		}
	}
	left := f.variantsOf(t, p.ID)
	if len(left) != len(variants) {
		t.Fatalf("整批拒绝后不应删除任一行：%d", len(left))
	}
	found := false
	for _, v := range left {
		if v.ID == unreferenced.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("无引用的行也不能被部分删除")
	}

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

func TestContainerSKUTwoPathsConsistent(t *testing.T) {
	f := newGuardFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 10.0

	self := f.mkProduct(t, "自建商品", "sku-consist-self", nil, &price)

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
