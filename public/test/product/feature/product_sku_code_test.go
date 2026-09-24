// Package feature product 模块 feature 测试 —— SKU 编码新规则（2026-09-19 评审第四轮）。
//
// 规则出处 docs/14-product-sku-and-cost-model.md §1 与 §4，覆盖五条：
//  1. 容器主体 SKU 从仓库选：<仓库短码大写>_<仓库里那条 SKU 原样>（不再追加序号）；
//  2. 自己创建的主体：运营填的编码 + 自动附加仓码前缀；
//  3. 变体 SKU = <主体>_<属性值段…>_V，属性段顺序固定（点选顺序不同得到同一个 SKU）；
//  4. 捆绑主体 SKU 恒以 _B 结尾（填了缺后缀补齐；没填=必填错误 ErrBundleSKURequired）；
//  5. 存量 SKU 一律不重写（新变体以老主体拼接）。
//
// 表结构一律来自生产迁移（迁移 246：products.sku_code + uq_products_project_sku_code），
// 不在 fixture 里手抄 DDL —— 手抄的「看起来像生产」会与迁移静默分叉。
package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

// skuFixture 隔离 PG schema + 生产迁移 + 真实工程 + 真实 inventory / product service。
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
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	wh, err := inv.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SZ", Name: "苏州仓",
	})
	if err != nil {
		t.Fatalf("建仓失败: %v", err)
	}
	return &skuFixture{inventory: inv, products: products, db: db, projectID: project.ID, warehouse: wh}
}

// containerSKUOf 直接读库取商品的主体 SKU（products.sku_code）。
func (f *skuFixture) containerSKUOf(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

// TestProductContainerSKUFromWarehouse 规则 ①：
// 从仓库选的主体 = <仓短码大写>_<仓库里那条 SKU 原样>，且首个变体就是容器主体本身。
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
	// 商品内唯一不变：同编码再建一个变体必须被拒。
	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, SKUCode: "TEE-001",
	}); err == nil {
		t.Fatal("同商品内重复的 SKU 应被拒绝")
	}
}

// TestVariantSKURemainsUniqueInProduct 变体 SKU 只在**商品内**唯一：
// 同一商品再加一个无规格变体时按 _002 让位（不报错、也不覆盖既有编码）；
// 而同工程内**另一个商品**用同一段编码不冲突（不要求全局唯一）。
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
	// 再加一个无规格变体：容器主体已被首个变体占用 → 按确定性序号让位。
	second, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: res.ID})
	if err != nil {
		t.Fatalf("第二个无规格变体不应失败: %v", err)
	}
	if second.SKUCode != "SZ_TEE-001_002" {
		t.Fatalf("同一商品内让位应得到 SZ_TEE-001_002，实际 %q", second.SKUCode)
	}
	// 运营显式给重复编码：明确拒绝（不是静默改写）。
	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, SKUCode: "TEE-001",
	}); err == nil {
		t.Fatal("同商品内重复的显式编码应被拒绝")
	}
	// 同一段仓库 SKU 在**另一个仓**被另一个商品引用：不冲突（「不同仓库可以有同一个 SKU」）。
	// 仓码前缀让两个主体编码天然不同，因此不触发工程内主体唯一索引。
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

// TestProductContainerSKUCustomWithPrefix 规则 ②：
// 自己创建的主体原样保留，选了仓自动附加仓码前缀；已带前缀时不重复拼接。
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

// mkSKUAttr 建一个「参与变体」的属性组。
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

// TestVariantSKUFixedOrderAndSuffix 规则 ③：
// 变体 SKU = 主体_属性值段…_V；属性段顺序固定，点选顺序不同也得到同一批 SKU。
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
	// 被承接的占位变体保留它创建时的 SKU（容器主体本身）—— 存量 / 已有 SKU 不重写。
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
	// 属性段必须全 ASCII 且以 _V 结尾。
	for combo, code := range skus {
		if combo == "red/s" {
			continue
		}
		if !strings.HasSuffix(code, "_V") {
			t.Fatalf("变体 SKU 应以 _V 结尾，实际 %q", code)
		}
	}

	// 点选顺序反过来（尺寸在前）重新生成：组合已存在 → 全部跳过，SKU 一个都不变。
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

// TestBundleContainerSKUAutoSuffix 规则 ④：捆绑主体恒以 _B 结尾 ——
// 填了但缺后缀 → 补齐；**没填 → 明确拒绝（ErrBundleSKURequired）**。
//
// 2026-09-19 用户拍板改掉「静默派生」：主体 SKU 是捆绑品的身份，必须让运营看见并确认；
// 新建抽屉按商品段预填建议值（可改、可一键重新生成），服务端不再接受空值。
func TestBundleContainerSKUAutoSuffix(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 129.0
	cases := map[string]string{
		"GIFT-BOX": "GIFT-BOX_B",
		"GIFT_B":   "GIFT_B",
	}
	for custom, want := range cases {
		t.Run(custom, func(t *testing.T) {
			res, err := f.products.Create(ctx, &productdto.CreateReq{
				ProjectID: f.projectID, Name: "捆绑 " + custom, Slug: "bundle-" + strings.ToLower(strings.ReplaceAll(custom, "_", "-")),
				Type: productmodel.TypeBundle, DefaultPrice: &price, SKUCode: custom,
			})
			if err != nil {
				t.Fatalf("创建捆绑商品失败: %v", err)
			}
			if got := f.containerSKUOf(t, res.ID); got != want {
				t.Fatalf("捆绑主体 SKU 应为 %q，实际 %q", want, got)
			}
			if len(res.Variants) != 0 {
				t.Fatalf("捆绑容器不生成变体，实际 %d 个", len(res.Variants))
			}
		})
	}

	// 没填主体编码：明确拒绝 —— 不再按商品 URL 段静默派生 <商品段>_B，
	// 也不退回随机码（运营必须看见并确认这个编码；抽屉会预填建议值）。
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "未填主体的捆绑", Slug: "bundle-empty",
		Type: productmodel.TypeBundle, DefaultPrice: &price,
	}); err == nil || err.Error() != productenums.ErrBundleSKURequired {
		t.Fatalf("没填主体应返回 %s，实际 %v", productenums.ErrBundleSKURequired, err)
	}
	// 中文 URL 段同理：填了就不看 URL 段，没填仍然是同一条必填错误（与「派生不出商品段」无关）。
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "中文捆绑", Slug: "中文捆绑",
		Type: productmodel.TypeBundle, DefaultPrice: &price,
	}); err == nil || err.Error() != productenums.ErrBundleSKURequired {
		t.Fatalf("中文 URL 段的捆绑没填主体同样应返回 %s，实际 %v", productenums.ErrBundleSKURequired, err)
	}
	// 中文 URL 段 + 显式填编码：编码与 URL 段无关，照样建成功。
	cn, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "中文捆绑", Slug: "中文捆绑",
		Type: productmodel.TypeBundle, DefaultPrice: &price, SKUCode: "CN-SET",
	})
	if err != nil {
		t.Fatalf("显式填了编码的捆绑不应报错: %v", err)
	}
	if got := f.containerSKUOf(t, cn.ID); got != "CN-SET_B" {
		t.Fatalf("显式编码应只补 _B 后缀得到 CN-SET_B，实际 %q", got)
	}
}

// TestExistingVariantSKUNotRewritten 规则 ⑤：
// 存量 SKU 一律不重写 —— 新生成的变体以老主体拼接，已有变体一个编码都不动。
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

	// 把它改造成「旧规则生成的存量数据」：主体与首个变体都退回 {仓码}_{商品码}_{序号}。
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

	// 给属性组加一个新值 → 只新增一个组合，它必须以**老主体**拼接，且带 _V 后缀。
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
	// 已有变体的编码一个都没变。
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

// TestContainerSKUExplicitError 派生不出主体段时**明确报错**（旧实现退回随机码）。
func TestContainerSKUExplicitError(t *testing.T) {
	f := newSKUFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	// 中文 URL 段 + 没有自定义编码 → 派生不出 ASCII 商品段，必须失败而不是随机编码。
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "中文商品", Slug: "中文商品", DefaultPrice: &price,
	}); err == nil {
		t.Fatal("中文 URL 段应明确报错，而不是退回随机码")
	}
	// 主体编码留空 = 「留空自动派生」，**不是**错误：按 URL 段派生（empty-sku → EMPTYSKU），
	// 再按归属仓兜底规则加上默认仓短码前缀（SZ_）。
	blank, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "留空主体商品", Slug: "empty-sku", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("留空主体编码应按规则派生，而不是报错: %v", err)
	}
	if got := f.containerSKUOf(t, blank.ID); got != "SZ_EMPTYSKU" {
		t.Fatalf("留空主体应派生 SZ_EMPTYSKU（默认仓兜底加的仓码前缀），实际 %q", got)
	}
	// 编辑路径显式给空串：明确拒绝（静默忽略会让运营以为改成功了）。
	empty := "  "
	if _, uerr := f.products.Update(ctx, &productdto.UpdateReq{
		ID: blank.ID, ProjectID: f.projectID, SKUCode: &empty,
	}); uerr == nil || uerr.Error() != productenums.ErrContainerSkuInvalid {
		t.Fatalf("编辑路径显式给空主体应返回 %s，实际 %v", productenums.ErrContainerSkuInvalid, uerr)
	}
	// 同工程内主体编码撞号：由唯一索引拒绝（商品侧不另写跨商品预检）。
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
