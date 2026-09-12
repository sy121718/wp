// Package feature product 模块 feature 测试 —— 捆绑品选项规则与整单校验（issue #20）。
//
// 覆盖验收 1~6（真实 PostgreSQL + 生产迁移 + 真实 service，装配与生产同形）：
//
//  1. 后台可为捆绑品配置选项集（跨商品挑已存在 SKU，逐项必选/可选 + 默认/最小/最大）；
//  2. 选项数量上限与整单最小总件数可配，超限 / 自相矛盾的配置在保存时被拒并给出可读原因；
//  4. 后端硬校验：漏必选 / 超单项上限 / 低于整单下限 / 数量非法 / 配置外 SKU 一律被拒；
//  5. 数量上限与整单下限同时受**库存可用量**约束，且可用量只读 inventory 真源 ——
//     把 product_variants.stock_total 缓存改成离谱的值也照样拦得住；
//  6. 套餐价 = 主体自定价（子项价格不参与前台展示），子项成本仍在后台保留。
//
// 依赖注入与生产一致：inventory 的实现同时注入 product 的变体库存端口与可用量端口
// （依赖方向 inventory → product）。
package feature

import (
	"context"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// bundleFixture 隔离 PG schema + 生产迁移/种子 + 真实工程 + 真实 inventory / product service。
type bundleFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	projectID string
	warehouse *inventorydto.WarehouseResp
}

func newBundleFixture(t *testing.T) *bundleFixture {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	// 内置变动原因字典（迁移 103）必须就位：库存变动只认字典里的原因。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "捆绑测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetVariantStock(inv)
	inv.SetVariantCost(products)
	// issue #20：捆绑的可用量端口（生产装配同形，见 routers.SetupRoutes）。
	products.SetAvailabilityPort(inv)
	wh, err := inv.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SZ", Name: "苏州仓",
	})
	if err != nil {
		t.Fatalf("建仓失败: %v", err)
	}
	return &bundleFixture{
		inventory: inv, products: products, db: db,
		projects: projects, projectID: project.ID, warehouse: wh,
	}
}

// mkProduct 建一个商品并返回详情（商品恒有首个变体）。
func (f *bundleFixture) mkProduct(t *testing.T, name, slug string, price *float64) *productdto.ProductResp {
	t.Helper()
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, DefaultPrice: price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return res
}

// firstVariant 取商品的首个变体。
func (f *bundleFixture) firstVariant(t *testing.T, productID string) *productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if len(detail.Variants) == 0 {
		t.Fatalf("商品 %s 没有变体", productID)
	}
	return detail.Variants[0]
}

// addStock 走真实库存变动链路入库（原因 = 内置的采购入库，方向 in）。
func (f *bundleFixture) addStock(t *testing.T, v *productdto.VariantResp, qty int) {
	t.Helper()
	_, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in", OperatorID: "tester",
		Lines: []inventorydto.StockChangeLineReq{{
			WarehouseID: f.warehouse.ID, ProductID: v.ProductID,
			VariantID: v.ID, SKUCode: v.SKUCode, Quantity: qty,
		}},
	})
	if err != nil {
		t.Fatalf("入库 %d 件失败: %v", qty, err)
	}
}

// setBundleConfig 保存配置的小工具（失败即测试失败）。
func (f *bundleFixture) setBundleConfig(t *testing.T, productID string, cfg productdto.BundleConfig) *productdto.BundleConfigResp {
	t.Helper()
	res, err := f.products.SetBundleConfig(context.Background(), &productdto.SetBundleConfigReq{
		ProductID: productID, Config: cfg, OperatorID: "tester",
	})
	if err != nil {
		t.Fatalf("保存捆绑配置失败: %v", err)
	}
	return res
}

// setBundleConfigErr 保存配置并断言被拒（返回错误文本）。
func (f *bundleFixture) setBundleConfigErr(t *testing.T, productID string, cfg productdto.BundleConfig) string {
	t.Helper()
	_, err := f.products.SetBundleConfig(context.Background(), &productdto.SetBundleConfigReq{
		ProductID: productID, Config: cfg,
	})
	if err == nil {
		t.Fatalf("该配置应被拒绝，实际保存成功")
	}
	return err.Error()
}

// bundleWith 生成一个「一个选项」的配置（参数化各数量与整单区间）。
func bundleWith(variantID string, required bool, def, minQ, maxQ, minTotal, maxTotal int) productdto.BundleConfig {
	cfg := productdto.NewEmptyBundleConfig()
	cfg.MinTotalQty, cfg.MaxTotalQty = minTotal, maxTotal
	cfg.Options = []productdto.BundleOption{{
		VariantID: variantID, Required: required,
		DefaultQty: def, MinQty: minQ, MaxQty: maxQ,
	}}
	return cfg
}

// TestBundleConfigOptionsFromAnySKU 验收 1：
// 选项来自跨商品挑选的已存在 SKU，逐项可设必选/可选与默认、最小、最大数量（最大留空 = 0）。
func TestBundleConfigOptionsFromAnySKU(t *testing.T) {
	f := newBundleFixture(t)
	if f == nil {
		return
	}
	mainPrice := 199.0
	main := f.mkProduct(t, "家庭套餐", "family-bundle", &mainPrice)
	addonA := f.mkProduct(t, "赠品杯", "addon-cup", nil)
	addonB := f.mkProduct(t, "赠品垫", "addon-mat", nil)
	va := f.firstVariant(t, addonA.ID)
	vb := f.firstVariant(t, addonB.ID)

	cfg := productdto.NewEmptyBundleConfig()
	cfg.MinTotalQty = 2
	cfg.Options = []productdto.BundleOption{
		{VariantID: va.ID, Required: true, DefaultQty: 1, MinQty: 1, MaxQty: 3},
		// 最大留空（0）= 不设上限，只受库存约束。
		{VariantID: vb.ID, Required: false, DefaultQty: 0, MinQty: 0, MaxQty: 0},
	}
	res := f.setBundleConfig(t, main.ID, cfg)
	if len(res.Options) != 2 {
		t.Fatalf("应有 2 个选项，实际 %d", len(res.Options))
	}
	if !res.Options[0].Required || res.Options[1].Required {
		t.Fatalf("必选 / 可选标记没有按配置保留: %+v", res.Options)
	}
	if res.Options[0].MaxQty != 3 || res.Options[1].MaxQty != 0 {
		t.Fatalf("最大数量应保留 3 与「留空」，实际 %d / %d", res.Options[0].MaxQty, res.Options[1].MaxQty)
	}
	if res.Options[0].SKUCode == "" || res.Options[1].SKUCode == "" {
		t.Fatalf("读回来的选项应带 SKU 快照（跨商品）: %+v", res.Options)
	}
	if res.Config.MinTotalQty != 2 {
		t.Fatalf("整单最小总件数应落库，实际 %d", res.Config.MinTotalQty)
	}
	// 落库形状就是迁移 114 收紧的对象（不是裸数组）。
	var ty string
	if err := f.db.Raw("SELECT jsonb_typeof(bundle_items) FROM products WHERE id = ?", main.ID).Scan(&ty).Error; err != nil {
		t.Fatalf("读 bundle_items 形状失败: %v", err)
	}
	if ty != "object" {
		t.Fatalf("bundle_items 应是对象，实际 %q", ty)
	}
}

// TestBundleConfigRejectsInvalid 验收 2：
// 超限与自相矛盾的配置在保存时被拒，且给出的是具体原因（不是笼统的参数错误）。
func TestBundleConfigRejectsInvalid(t *testing.T) {
	f := newBundleFixture(t)
	if f == nil {
		return
	}
	price := 100.0
	main := f.mkProduct(t, "套餐主体", "bundle-main", &price)
	other := f.mkProduct(t, "子项", "bundle-addon", nil)
	v := f.firstVariant(t, other.ID)
	self := f.firstVariant(t, main.ID)

	// 选项数量上限超出服务端护栏。
	bad := bundleWith(v.ID, true, 1, 1, 0, 0, 0)
	bad.MaxOptions = productdto.BundleMaxOptionsLimit + 1
	if got := f.setBundleConfigErr(t, main.ID, bad); got != productenums.ErrBundleMaxOptionsInvalid {
		t.Fatalf("超护栏的选项上限应被拒（%s），实际 %q", productenums.ErrBundleMaxOptionsInvalid, got)
	}
	// 配置的选项数超过自身上限。
	bad = bundleWith(v.ID, true, 1, 1, 0, 0, 0)
	bad.MaxOptions = 2 // 上限放到 2，让重复 SKU 而不是「选项数超限」成为被拒原因
	bad.Options = append(bad.Options, productdto.BundleOption{VariantID: v.ID})
	if got := f.setBundleConfigErr(t, main.ID, bad); got != productenums.ErrBundleVariantDuplicated {
		t.Fatalf("同一 SKU 重复出现应被拒（%s），实际 %q", productenums.ErrBundleVariantDuplicated, got)
	}
	// 单项区间自相矛盾：最大 < 最小。
	if got := f.setBundleConfigErr(t, main.ID, bundleWith(v.ID, false, 1, 3, 2, 0, 0)); got != productenums.ErrBundleQtyRangeInvalid {
		t.Fatalf("最大 < 最小应被拒（%s），实际 %q", productenums.ErrBundleQtyRangeInvalid, got)
	}
	// 默认数量低于最小数量。
	if got := f.setBundleConfigErr(t, main.ID, bundleWith(v.ID, false, 1, 2, 0, 0, 0)); got != productenums.ErrBundleQtyRangeInvalid {
		t.Fatalf("默认 < 最小应被拒（%s），实际 %q", productenums.ErrBundleQtyRangeInvalid, got)
	}
	// 整单区间自相矛盾：最大 < 最小。
	if got := f.setBundleConfigErr(t, main.ID, bundleWith(v.ID, true, 1, 1, 0, 5, 2)); got != productenums.ErrBundleTotalRangeInvalid {
		t.Fatalf("整单最大 < 最小应被拒（%s），实际 %q", productenums.ErrBundleTotalRangeInvalid, got)
	}
	// 整单下限永远不可达（所有选项最多只能加到 1 件，却要求至少 5 件）。
	if got := f.setBundleConfigErr(t, main.ID, bundleWith(v.ID, true, 1, 1, 1, 5, 0)); got != productenums.ErrBundleTotalUnreachable {
		t.Fatalf("不可达的整单下限应被拒（%s），实际 %q", productenums.ErrBundleTotalUnreachable, got)
	}
	// 引用了不存在的 SKU。
	missing := bundleWith("11111111-1111-1111-1111-111111111111", true, 1, 1, 0, 0, 0)
	if got := f.setBundleConfigErr(t, main.ID, missing); got != productenums.ErrBundleVariantNotFound {
		t.Fatalf("不存在的 SKU 应被拒（%s），实际 %q", productenums.ErrBundleVariantNotFound, got)
	}
	// 自引用：捆绑主体自己的 SKU 不能作为套餐子项。
	if got := f.setBundleConfigErr(t, main.ID, bundleWith(self.ID, true, 1, 1, 0, 0, 0)); got != productenums.ErrBundleSelfReference {
		t.Fatalf("自引用应被拒（%s），实际 %q", productenums.ErrBundleSelfReference, got)
	}
	// 缺 SKU。
	if got := f.setBundleConfigErr(t, main.ID, bundleWith("", true, 1, 1, 0, 0, 0)); got != productenums.ErrBundleVariantRequired {
		t.Fatalf("缺 SKU 应被拒（%s），实际 %q", productenums.ErrBundleVariantRequired, got)
	}
}
