package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

type skuFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projectID string
	warehouse *inventorydto.WarehouseResp
}

func newSKUFixture(t *testing.T) *skuFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "SKU 编码测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)
	wh, err := inv.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SZ", Name: "苏州仓",
	})
	if err != nil {
		t.Fatalf("建仓失败: %v", err)
	}
	return &skuFixture{inventory: inv, products: products, db: db, projectID: project.ID, warehouse: wh}
}

func (f *skuFixture) containerSKUOf(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

func TestProductContainerSKUFromWarehouse(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	res, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "仓库选品", Slug: "warehouse-pick",
		DefaultPrice: &price, SKUCode: "TEE-001",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if got := f.containerSKUOf(t, res.ID); got != "SZ_TEE-001" {
		t.Fatalf("主体 SKU 应为 SZ_TEE-001（仓码大写 + 仓库 SKU 原样），实际 %q", got)
	}
	if len(res.Variants) != 1 || res.Variants[0].SKUCode != "SZ_TEE-001" {
		t.Fatalf("首个变体应等于容器主体，实际 %+v", res.Variants)
	}

	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, SKUCode: "TEE-001",
	}); err == nil {
		t.Fatal("同商品内重复的 SKU 应被拒绝")
	}
}

func TestVariantSKURemainsUniqueInProduct(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	res, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "重复变体商品", Slug: "dup-variant",
		DefaultPrice: &price, SKUCode: "TEE-001",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	second, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: res.ID})
	if err != nil {
		t.Fatalf("第二个无规格变体不应失败: %v", err)
	}
	if second.SKUCode != "SZ_TEE-001_002" {
		t.Fatalf("同一商品内让位应得到 SZ_TEE-001_002，实际 %q", second.SKUCode)
	}

	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, SKUCode: "TEE-001",
	}); err == nil {
		t.Fatal("同商品内重复的显式编码应被拒绝")
	}

	sh, err := f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "SH", Name: "上海仓",
	})
	if err != nil {
		t.Fatalf("建第二个仓失败: %v", err)
	}
	other, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "另一个仓的商品", Slug: "another",
		DefaultPrice: &price, SKUCode: "TEE-001", WarehouseID: sh.ID,
	})
	if err != nil {
		t.Fatalf("另一个仓引用同一段仓库 SKU 不应失败: %v", err)
	}
	if got := f.containerSKUOf(t, other.ID); got != "SH_TEE-001" {
		t.Fatalf("主体应带自己的仓码前缀 SH_TEE-001，实际 %q", got)
	}
}

func TestProductContainerSKUCustomWithPrefix(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	cases := map[string]string{
		"my-tee":     "SZ_my-tee",
		"sz_already": "sz_already",
		"CUSTOM-001": "SZ_CUSTOM-001",
	}
	for custom, want := range cases {
		t.Run(custom, func(t *testing.T) {
			res, err := f.products.Create(ctx, &productdto.CreateReq{
				ProjectID: f.projectID, Name: "自定义主体 " + custom, Slug: "custom-" + strings.ToLower(strings.ReplaceAll(custom, "_", "-")),
				DefaultPrice: &price, SKUCode: custom,
			})
			if err != nil {
				t.Fatalf("创建商品失败: %v", err)
			}
			if got := f.containerSKUOf(t, res.ID); got != want {
				t.Fatalf("主体 SKU 应为 %q，实际 %q", want, got)
			}
		})
	}
}

func mkSKUAttr(t *testing.T, f *skuFixture, name, key string, valueKeys []string) *productdto.AttributeResp {
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

func TestVariantSKUFixedOrderAndSuffix(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "组合商品", Slug: "combo",
		DefaultPrice: &price, SKUCode: "TEE",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if got := f.containerSKUOf(t, p.ID); got != "SZ_TEE" {
		t.Fatalf("主体 SKU 应为 SZ_TEE，实际 %q", got)
	}
	color := mkSKUAttr(t, f, "颜色", "color", []string{"red", "blue"})
	size := mkSKUAttr(t, f, "尺寸", "size", []string{"s", "m"})
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, AttributeIDs: []string{color.ID, size.ID},
	}); err != nil {
		t.Fatalf("挂属性组失败: %v", err)
	}

	gen := func(selections []productdto.VariantSelectionReq) *productdto.GenerateVariantsResp {
		t.Helper()
		res, gerr := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{
			ProductID: p.ID, Selections: selections,
		})
		if gerr != nil {
			t.Fatalf("生成变体失败: %v", gerr)
		}
		return res
	}
	first := gen(nil)
	if first.Total != 4 || first.Created != 3 || first.Adopted != 1 {
		t.Fatalf("2×2 应得到 4 个组合（承接 1 + 新建 3），实际 total=%d created=%d adopted=%d",
			first.Total, first.Created, first.Adopted)
	}
	skus := map[string]string{}
	for _, v := range first.Variants {
		var opts map[string]string
		if uerr := json.Unmarshal(v.OptionValues, &opts); uerr != nil {
			t.Fatalf("组合解析失败: %v", uerr)
		}
		skus[opts["color"]+"/"+opts["size"]] = v.SKUCode
	}

	if got := skus["red/s"]; got != "SZ_TEE" {
		t.Fatalf("被承接的组合应保留容器主体的编码，实际 %q", got)
	}
	want := map[string]string{
		"red/m":  "SZ_TEE_red_m_V",
		"blue/s": "SZ_TEE_blue_s_V",
		"blue/m": "SZ_TEE_blue_m_V",
	}
	for combo, code := range want {
		if skus[combo] != code {
			t.Fatalf("组合 %s 的 SKU 应为 %q，实际 %q", combo, code, skus[combo])
		}
	}

	for combo, code := range skus {
		if combo == "red/s" {
			continue
		}
		if !strings.HasSuffix(code, "_V") {
			t.Fatalf("变体 SKU 应以 _V 结尾，实际 %q", code)
		}
	}

	second := gen([]productdto.VariantSelectionReq{
		{AttributeID: size.ID, ValueIDs: []string{size.Values[0].ID, size.Values[1].ID}},
		{AttributeID: color.ID, ValueIDs: []string{color.Values[0].ID, color.Values[1].ID}},
	})
	if second.Created != 0 || second.Skipped != 4 {
		t.Fatalf("点选顺序不同不应生成新 SKU：created=%d skipped=%d", second.Created, second.Skipped)
	}
	for _, v := range second.Variants {
		var opts map[string]string
		if uerr := json.Unmarshal(v.OptionValues, &opts); uerr != nil {
			t.Fatalf("组合解析失败: %v", uerr)
		}
		if got, wantCode := skus[opts["color"]+"/"+opts["size"]], v.SKUCode; got != wantCode {
			t.Fatalf("顺序不同但组合相同必须得到同一个 SKU：%q vs %q", wantCode, got)
		}
	}
}

func TestExistingVariantSKUNotRewritten(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "存量商品", Slug: "legacy",
		DefaultPrice: &price, SKUCode: "TEE",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	color := mkSKUAttr(t, f, "颜色", "color", []string{"red", "blue"})
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, AttributeIDs: []string{color.ID},
	}); err != nil {
		t.Fatalf("挂属性组失败: %v", err)
	}
	if _, err := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID}); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}

	if err := f.db.Exec("UPDATE products SET sku_code = 'SZ_LEGACY_001' WHERE id = ?", p.ID).Error; err != nil {
		t.Fatalf("回写存量主体失败: %v", err)
	}
	if err := f.db.Exec("UPDATE product_variants SET sku_code = 'SZ_LEGACY_001' WHERE product_id = ? AND option_values = '{}'::jsonb", p.ID).Error; err != nil {
		t.Fatalf("回写存量变体失败: %v", err)
	}
	before, err := f.products.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	beforeSKUs := map[string]bool{}
	for _, v := range before.Variants {
		beforeSKUs[v.SKUCode] = true
	}

	if _, err := f.products.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: color.ID,
		Values: []productdto.AttributeValueReq{
			{ID: color.Values[0].ID, Label: "红", Key: "red"},
			{ID: color.Values[1].ID, Label: "蓝", Key: "blue"},
			{Label: "绿", Key: "green"},
		},
	}); err != nil {
		t.Fatalf("追加属性值失败: %v", err)
	}
	res, err := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("追加值后生成失败: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("追加一个值应只新增一个变体，实际 %d", res.Created)
	}
	created := ""
	for _, v := range res.Variants {
		if !beforeSKUs[v.SKUCode] {
			created = v.SKUCode
		}
	}
	if created != "SZ_LEGACY_001_green_V" {
		t.Fatalf("新变体应以存量主体拼接，期望 SZ_LEGACY_001_green_V，实际 %q", created)
	}

	after, err := f.products.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	for _, v := range after.Variants {
		if v.SKUCode == created {
			continue
		}
		if !beforeSKUs[v.SKUCode] {
			t.Fatalf("存量变体的 SKU 被重写了：%q", v.SKUCode)
		}
	}
}

func TestContainerSKUExplicitError(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0

	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "中文商品", Slug: "中文商品", DefaultPrice: &price,
	}); err == nil {
		t.Fatal("中文 URL 段应明确报错，而不是退回随机码")
	}

	blank, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "留空主体商品", Slug: "empty-sku", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("留空主体编码应按规则派生，而不是报错: %v", err)
	}
	if got := f.containerSKUOf(t, blank.ID); got != "SZ_EMPTYSKU" {
		t.Fatalf("留空主体应派生 SZ_EMPTYSKU（默认仓兜底加的仓码前缀），实际 %q", got)
	}

	empty := "  "
	if _, uerr := f.products.Update(ctx, &productdto.UpdateReq{
		ID: blank.ID, ProjectID: f.projectID, SKUCode: &empty,
	}); uerr == nil || uerr.Error() != productenums.ErrContainerSkuInvalid {
		t.Fatalf("编辑路径显式给空主体应返回 %s，实际 %v", productenums.ErrContainerSkuInvalid, uerr)
	}

	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "撞号 A", Slug: "dup-a", DefaultPrice: &price, SKUCode: "DUP",
	}); err != nil {
		t.Fatalf("第一个商品不应失败: %v", err)
	}
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "撞号 B", Slug: "dup-b", DefaultPrice: &price, SKUCode: "DUP",
	}); err == nil {
		t.Fatal("同工程内重复的主体编码应被唯一索引拒绝")
	}
}
