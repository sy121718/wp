// product_bundle_member_source_test.go — 捆绑成员的三种来源（docs/14 §1.2，批次 C）。
//
// 三种来源（从商品导入 / 从仓库选 / 自选属性值笛卡尔积）共用一条出口 ResolveBundleMembers，
// 因此本文件按**共用口径**组织断言，而不是每条来源各写一套：
//
//  1. 三种来源各自的成功路径（成员身份恒为 variantId）；
//  2. 去重：同一变体在成员清单里只出现一次（库里已有 + 本批已解析 + 前端清单里的）；
//  3. 属性组合在商品侧没有对应变体时**逐条拒绝并给出原因**（不静默丢弃、不造无变体成员）；
//  4. 单条失败不整批失败（一口锅不端走：该仓没有那条货 / 变体已停用 / 超上限都只跳过它）；
//  5. 来源快照（sourceKind / warehouseId / warehouseSku / externalSku）落库与回显。
//
// 真实 PostgreSQL + 生产迁移/种子 + 真实 product / inventory service（装配与生产同形：
// SetInventoryService + SetInventory + SetAvailabilityPort 三个端口都要接，否则守卫或
// 配置读模型会静默退化）。
package feature

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorydto "go_wp/internal/module/product/inventory/dto"
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

// memberSrcFixture 隔离 PG schema + 生产迁移/种子 + 真实工程、商品与库存三个 service。
type memberSrcFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projectID string
	// sz 是默认仓（商品创建时的首个变体落在这里）；sh 是第二个仓（仓库来源的用例都在它上面）。
	sz *inventorydto.WarehouseResp
	sh *inventorydto.WarehouseResp
}

func newMemberSrcFixture(t *testing.T) *memberSrcFixture {
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
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "捆绑成员来源测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	products.SetInventory(inventorymodel.NewModel(db))
	products.SetAvailabilityPort(inv)
	inv.SetVariantCost(products)
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
	return &memberSrcFixture{
		inventory: inv, products: products, db: db,
		projectID: project.ID, sz: sz, sh: sh,
	}
}

// mkAttr 建一个「参与变体」的属性组（值与 key 一一对应）。
func (f *memberSrcFixture) mkAttr(t *testing.T, name, key string, valueKeys []string) *productdto.AttributeResp {
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
func (f *memberSrcFixture) mkProduct(t *testing.T, name, slug string, attributeIDs []string, price *float64) *productdto.ProductResp {
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

// mkBundle 建一个捆绑容器（无变体、只有一个容器价、主体 SKU 以 _B 结尾）。
func (f *memberSrcFixture) mkBundle(t *testing.T, name, slug, sku string, price float64) *productdto.ProductResp {
	t.Helper()
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		Type: productmodel.TypeBundle, SKUCode: sku, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("建捆绑容器 %s 失败: %v", name, err)
	}
	return res
}

// variantsOf 读商品的全部变体。
func (f *memberSrcFixture) variantsOf(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail.Variants
}

// generateAll 走「预览—保存」生成该商品的全部组合（与后台抽屉同一条路径）。
func (f *memberSrcFixture) generateAll(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	ctx := context.Background()
	pv, err := f.products.PreviewVariantCombinations(ctx, &productdto.PreviewVariantReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
	})
	if err != nil {
		t.Fatalf("预览组合失败: %v", err)
	}
	rows := make([]productdto.VariantListRow, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListRow{OptionValues: row.OptionValues})
	}
	if _, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
		OperatorID: "tester", Rows: rows,
	}); err != nil {
		t.Fatalf("保存变体清单失败: %v", err)
	}
	return f.variantsOf(t, productID)
}

// disableVariant 停用某个变体（「已停用的变体不进成员」用例）。
func (f *memberSrcFixture) disableVariant(t *testing.T, variantID string) {
	t.Helper()
	off := false
	if _, err := f.products.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: variantID, Enabled: &off, ProjectID: f.projectID,
	}); err != nil {
		t.Fatalf("停用变体失败: %v", err)
	}
}

// addStock 走真实库存变动链路入库（原因 = 内置的采购入库）。
func (f *memberSrcFixture) addStock(t *testing.T, v *productdto.VariantResp, qty int) {
	t.Helper()
	if _, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, WarehouseID: f.sz.ID,
		Direction: "in", ReasonCode: "purchase_in", OperatorID: "tester",
		Lines: []inventorydto.StockChangeLineReq{{
			WarehouseID: f.sz.ID, ProductID: v.ProductID,
			VariantID: v.ID, SKUCode: v.SKUCode, Quantity: qty,
		}},
	}); err != nil {
		t.Fatalf("入库 %d 件失败: %v", qty, err)
	}
}

// seedWarehouseRow 在指定仓给某个变体登记一条仓库 SKU（inventory_stocks 的一行）。
func (f *memberSrcFixture) seedWarehouseRow(t *testing.T, warehouseID, skuCode string, v *productdto.VariantResp) {
	t.Helper()
	if _, err := f.inventory.EnsureStock(context.Background(), &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID,
		ProductID: v.ProductID, VariantID: v.ID, SKUCode: skuCode,
	}); err != nil {
		t.Fatalf("登记仓库 SKU %s 失败: %v", skuCode, err)
	}
}

// bindExternal 给某 (仓库, 变体) 行登记外部编码（第三方仓的编码，迁移 251）。
func (f *memberSrcFixture) bindExternal(t *testing.T, warehouseID, variantID, external string) {
	t.Helper()
	if _, err := f.inventory.BindExternalSKU(context.Background(), &inventorydto.BindExternalSKUReq{
		ProjectID: f.projectID, WarehouseID: warehouseID, VariantID: variantID, ExternalSKU: external,
	}); err != nil {
		t.Fatalf("登记外部编码 %s 失败: %v", external, err)
	}
}

// resolve 解析一批候选成员（失败即测试失败）。
func (f *memberSrcFixture) resolve(t *testing.T, req *productdto.ResolveBundleMembersReq) *productdto.ResolveBundleMembersResp {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = f.projectID
	}
	res, err := f.products.ResolveBundleMembers(context.Background(), req)
	if err != nil {
		t.Fatalf("解析成员来源失败: %v", err)
	}
	return res
}

// resolveErr 解析并返回错误文案（期望失败时用）。
func (f *memberSrcFixture) resolveErr(t *testing.T, req *productdto.ResolveBundleMembersReq) string {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID = f.projectID
	}
	_, err := f.products.ResolveBundleMembers(context.Background(), req)
	if err == nil {
		t.Fatalf("该来源请求应被拒绝，实际成功")
	}
	return err.Error()
}

// saveConfig 保存捆绑配置（失败即测试失败）。
func (f *memberSrcFixture) saveConfig(t *testing.T, productID string, cfg productdto.BundleConfig) *productdto.BundleConfigResp {
	t.Helper()
	res, err := f.products.SetBundleConfig(context.Background(), &productdto.SetBundleConfigReq{
		ProductID: productID, ProjectID: f.projectID, Config: cfg, OperatorID: "tester",
	})
	if err != nil {
		t.Fatalf("保存捆绑配置失败: %v", err)
	}
	return res
}

// bundleItemsRaw 直读 products.bundle_items（落库形状的唯一真源，不走 service 响应）。
func (f *memberSrcFixture) bundleItemsRaw(t *testing.T, productID string) string {
	t.Helper()
	var raw string
	if err := f.db.Raw("SELECT bundle_items::text FROM products WHERE id = ?", productID).Scan(&raw).Error; err != nil {
		t.Fatalf("读 bundle_items 失败: %v", err)
	}
	return raw
}

// skipReasons 统计跳过原因（key → 条数）。
func skipReasons(res *productdto.ResolveBundleMembersResp) map[string]int {
	out := map[string]int{}
	for _, s := range res.Skipped {
		out[s.Reason]++
	}
	return out
}

// memberIDs 取候选成员的变体 id 集合。
func memberIDs(res *productdto.ResolveBundleMembersResp) []string {
	out := make([]string, 0, len(res.Members))
	for _, m := range res.Members {
		out = append(out, m.VariantID)
	}
	return out
}

// —— ① 从商品导入：整商品（全部启用变体）导入为成员 + 去重 + 上限 + 停用跳过 ——

// TestBundleMemberSourceProductImport ①成功路径：
// 选中一个商品 → 它的启用变体一次导入为成员；停用的逐条跳过；整批不失败。
func TestBundleMemberSourceProductImport(t *testing.T) {
	f := newMemberSrcFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	price := 30.0
	src := f.mkProduct(t, "来源商品", "member-src-product", []string{color.ID, size.ID}, &price)
	variants := f.generateAll(t, src.ID)
	if len(variants) != 4 {
		t.Fatalf("来源商品应有 4 个规格变体，实际 %d", len(variants))
	}
	bundle := f.mkBundle(t, "套餐", "member-bundle-product", "MEMBER_B", 99.0)

	res := f.resolve(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceProduct, SourceProductID: src.ID,
	})
	if len(res.Members) != 4 || len(res.Skipped) != 0 {
		t.Fatalf("整商品导入应导入全部 4 个启用变体，实际 %d 个 / 跳过 %d 条", len(res.Members), len(res.Skipped))
	}
	for _, m := range res.Members {
		if m.VariantID == "" || m.SKUCode == "" || m.ProductName != "来源商品" {
			t.Fatalf("候选项应带变体身份与所属商品名：%+v", m)
		}
		if m.Source.Kind != productenums.BundleSourceProduct {
			t.Fatalf("来源快照应是商品导入，实际 %+v", m.Source)
		}
		if string(m.OptionValues) == "" || string(m.OptionValues) == "null" {
			t.Fatalf("候选项应带规格组合（前端据此展示），实际 %q", string(m.OptionValues))
		}
	}

	// 停用一个变体：其余照常导入，停用的那一条只跳过（不整批失败）。
	target := variants[0]
	f.disableVariant(t, target.ID)
	res = f.resolve(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceProduct, SourceProductID: src.ID,
	})
	if len(res.Members) != 3 {
		t.Fatalf("停用一个变体后应导入 3 个，实际 %d", len(res.Members))
	}
	reasons := skipReasons(res)
	if reasons[productenums.BundleMemberVariantDisabled] != 1 {
		t.Fatalf("停用的变体应记一条 %s，实际 %+v", productenums.BundleMemberVariantDisabled, res.Skipped)
	}

	// 去重：前端清单里已有的成员不再追加（服务端按 variantId 判，前端提交的形状不作数）。
	res = f.resolve(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceProduct, SourceProductID: src.ID,
		ExistingVariantIDs: memberIDs(res),
	})
	if len(res.Members) != 0 {
		t.Fatalf("清单里已有的成员不应重复解析出来，实际 %d 个", len(res.Members))
	}
	if got := skipReasons(res)[productenums.BundleMemberSkippedInList]; got != 3 {
		t.Fatalf("3 个已在清单里的成员都应记 %s，实际 %d 条：%+v",
			productenums.BundleMemberSkippedInList, got, res.Skipped)
	}

	// 上限：容器配置的 maxOptions 决定本次能追加多少条，多出来的逐条说明（不静默截断）。
	cfg := productdto.NewEmptyBundleConfig()
	cfg.MaxOptions = 2
	f.saveConfig(t, bundle.ID, cfg)
	res = f.resolve(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceProduct, SourceProductID: src.ID,
	})
	if len(res.Members) != 2 {
		t.Fatalf("上限 2 时应只解析出 2 个候选，实际 %d", len(res.Members))
	}
	reasons = skipReasons(res)
	if reasons[productenums.BundleMemberOptionsExceeded] != 1 || reasons[productenums.BundleMemberVariantDisabled] != 1 {
		t.Fatalf("超出上限 / 已停用各应记一条原因，实际 %+v", res.Skipped)
	}
}

// —— ② 从仓库选：按仓定位变体 + 来源快照落库与回显 + 单条失败不整批失败 ——

// TestBundleMemberSourceWarehouse ②成功路径：
// (仓库, 仓库 SKU) 定位到那条货的变体，来源快照（仓库 / 仓库 SKU / 外部编码）随成员落库并可回显；
// 该仓没有这条货时只跳过它，其余照常解析。
func TestBundleMemberSourceWarehouse(t *testing.T) {
	f := newMemberSrcFixture(t)
	if f == nil {
		return
	}
	price := 15.0
	item := f.mkProduct(t, "仓库货", "member-src-warehouse", nil, &price)
	v := f.variantsOf(t, item.ID)[0]
	// 第二个仓的同一变体：仓库 SKU 用另一个编码（(仓库, 变体) 一行，SKU 仓内唯一）。
	f.seedWarehouseRow(t, f.sh.ID, "WH-ITEM-1", v)
	f.bindExternal(t, f.sh.ID, v.ID, "EXT-ITEM-1")
	bundle := f.mkBundle(t, "仓库套餐", "member-bundle-warehouse", "WH_B", 88.0)

	req := func() *productdto.ResolveBundleMembersReq {
		return &productdto.ResolveBundleMembersReq{
			ProductID: bundle.ID, Source: productenums.BundleSourceWarehouse,
			WarehouseID: f.sh.ID, WarehouseSKUs: []string{"WH-ITEM-1", "NOPE-1"},
		}
	}
	res := f.resolve(t, req())
	if len(res.Members) != 1 {
		t.Fatalf("应解析出 1 个成员（另一条仓库 SKU 不存在），实际 %d：%+v", len(res.Members), res.Skipped)
	}
	m := res.Members[0]
	if m.VariantID != v.ID {
		t.Fatalf("成员身份应是那条货的变体 %s，实际 %s", v.ID, m.VariantID)
	}
	if m.Source.Kind != productenums.BundleSourceWarehouse ||
		m.Source.WarehouseID != f.sh.ID || m.Source.WarehouseSKU != "WH-ITEM-1" || m.Source.ExternalSKU != "EXT-ITEM-1" {
		t.Fatalf("来源快照不完整：%+v", m.Source)
	}
	// 单条失败不整批失败：不存在的那条逐条回带原因。
	reasons := skipReasons(res)
	if reasons[productenums.BundleMemberWarehouseSKUMissing] != 1 {
		t.Fatalf("该仓没有这条货应记一条 %s，实际 %+v", productenums.BundleMemberWarehouseSKUMissing, res.Skipped)
	}

	// 落库：把候选项（带来源快照）原样保存 → 配置里能读回来，jsonb 里也确实写了。
	cfg := productdto.NewEmptyBundleConfig()
	cfg.Options = []productdto.BundleOption{{
		VariantID: m.VariantID, Required: true, DefaultQty: 1, MinQty: 1,
		SourceKind: m.Source.Kind, WarehouseID: m.Source.WarehouseID,
		WarehouseSKU: m.Source.WarehouseSKU, ExternalSKU: m.Source.ExternalSKU,
	}}
	saved := f.saveConfig(t, bundle.ID, cfg)
	if len(saved.Options) != 1 {
		t.Fatalf("配置应落库 1 个成员，实际 %d", len(saved.Options))
	}
	got := saved.Options[0]
	if got.SourceKind != productenums.BundleSourceWarehouse || got.WarehouseID != f.sh.ID ||
		got.WarehouseSKU != "WH-ITEM-1" || got.ExternalSKU != "EXT-ITEM-1" {
		t.Fatalf("回显的来源快照与落库的不一致：%+v", got.BundleOption)
	}
	raw := f.bundleItemsRaw(t, bundle.ID)
	// jsonb 的 ::text 输出在冒号后带一个空格（PG 的既定格式），断言按它的原样写。
	for _, want := range []string{"\"sourceKind\": \"BundleSourceWarehouse\"", "\"warehouseSku\": \"WH-ITEM-1\"", "\"externalSku\": \"EXT-ITEM-1\""} {
		if !strings.Contains(raw, want) {
			t.Fatalf("bundle_items 里应有 %s，实际 %s", want, raw)
		}
	}

	// 来源归一是服务端的责任：非仓库来源带着仓库字段进来 → 仓库字段被清空。
	cfg.Options[0].SourceKind = productenums.BundleSourceProduct
	saved = f.saveConfig(t, bundle.ID, cfg)
	if got := saved.Options[0]; got.WarehouseID != "" || got.WarehouseSKU != "" || got.ExternalSKU != "" {
		t.Fatalf("非仓库来源的仓库字段应被清空，实际 %+v", got.BundleOption)
	}
	// 未知来源明确拒绝（不静默降级成「手工指定」）。
	cfg.Options[0].SourceKind = "nope"
	if _, err := f.products.SetBundleConfig(context.Background(), &productdto.SetBundleConfigReq{
		ProductID: bundle.ID, ProjectID: f.projectID, Config: cfg, OperatorID: "tester",
	}); err == nil || !strings.Contains(err.Error(), productenums.ErrBundleSourceInvalid) {
		t.Fatalf("未知来源应报 %s，实际 %v", productenums.ErrBundleSourceInvalid, err)
	}
}

// —— ③ 自选属性值组合：服务端重算 + 不存在的组合逐条拒绝 ——

// TestBundleMemberSourceAttributesRejectsMissingCombo ③：
// 组合由服务端按属性组固定顺序重算；商品侧没有对应变体的组合**逐条拒绝并给出原因**，
// 既不静默丢弃，也不造一个无变体的成员。
func TestBundleMemberSourceAttributesRejectsMissingCombo(t *testing.T) {
	f := newMemberSrcFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	price := 20.0
	src := f.mkProduct(t, "组合来源商品", "member-src-attr", []string{color.ID, size.ID}, &price)

	// 只生成 2 个组合（red_s / red_m）：blue 的两种规格在商品侧**没有变体**。
	ctx := context.Background()
	pv, err := f.products.PreviewVariantCombinations(ctx, &productdto.PreviewVariantReq{
		ProductID: src.ID, ProjectID: f.projectID, WarehouseID: f.sz.ID,
		Selections: []productdto.VariantSelectionReq{{AttributeID: color.ID, ValueIDs: []string{f.valueID(t, color, "red")}}},
	})
	if err != nil {
		t.Fatalf("预览组合失败: %v", err)
	}
	rows := make([]productdto.VariantListRow, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListRow{OptionValues: row.OptionValues})
	}
	if _, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: src.ID, ProjectID: f.projectID, WarehouseID: f.sz.ID, OperatorID: "tester", Rows: rows,
	}); err != nil {
		t.Fatalf("保存变体清单失败: %v", err)
	}
	before := len(f.variantsOf(t, src.ID))
	if before != 2 {
		t.Fatalf("商品侧应只有 2 个变体，实际 %d", before)
	}
	bundle := f.mkBundle(t, "组合套餐", "member-bundle-attr", "ATTR_B", 66.0)

	// 勾选两个颜色 × 两个尺寸 = 4 个组合，其中 2 个在商品侧没有变体。
	selections := []productdto.VariantSelectionReq{
		{AttributeID: color.ID, ValueIDs: []string{
			f.valueID(t, color, "red"), f.valueID(t, color, "blue"), f.valueID(t, color, "red"), // 重复勾选同一值 → 去重
		}},
		{AttributeID: size.ID, ValueIDs: []string{f.valueID(t, size, "s"), f.valueID(t, size, "m")}},
	}
	res := f.resolve(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceAttributes,
		SourceProductID: src.ID, Selections: selections,
	})
	if res.Total != 4 {
		t.Fatalf("去重后的组合总数应是 4（重复勾选不算两次），实际 %d", res.Total)
	}
	if len(res.Members) != 2 {
		t.Fatalf("商品侧存在的 2 个组合应成为候选，实际 %d", len(res.Members))
	}
	if len(res.Skipped) != 2 {
		t.Fatalf("不存在的 2 个组合应逐条拒绝，实际 %d：%+v", len(res.Skipped), res.Skipped)
	}
	for _, s := range res.Skipped {
		if s.Reason != productenums.BundleMemberNotOnProduct {
			t.Fatalf("不存在的组合应报 %s，实际 %+v", productenums.BundleMemberNotOnProduct, s)
		}
		if len(s.OptionValues) == 0 || string(s.OptionValues) == "null" {
			t.Fatalf("被拒绝的组合必须带组合本体（页面要指得出是哪一组），实际 %+v", s)
		}
		if s.VariantID != "" {
			t.Fatalf("被拒绝的组合不该有变体身份（绝不造无变体成员），实际 %+v", s)
		}
	}
	// 拒绝不等于落库：变体总数一个都没变（也不会有「顺手生成一个」的副作用）。
	if after := len(f.variantsOf(t, src.ID)); after != before {
		t.Fatalf("解析不应新建变体，实际 %d → %d", before, after)
	}
	// 服务端重算：勾选里出现组内不存在的值时明确报错，而不是丢掉那一项。
	bad := []productdto.VariantSelectionReq{{AttributeID: color.ID, ValueIDs: []string{"v-none"}}}
	if got := f.resolveErr(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceAttributes,
		SourceProductID: src.ID, Selections: bad,
	}); !strings.Contains(got, productenums.ErrVariationValueInvalid) {
		t.Fatalf("非法勾选应报 %s，实际 %q", productenums.ErrVariationValueInvalid, got)
	}
}

// valueID 取属性组内某个 key 的值 id。
func (f *memberSrcFixture) valueID(t *testing.T, attr *productdto.AttributeResp, key string) string {
	t.Helper()
	for _, v := range attr.Values {
		if v.Key == key {
			return v.ID
		}
	}
	t.Fatalf("属性组 %s 里没有值 %s", attr.Key, key)
	return ""
}

// —— 请求级校验：来源不合法 / 缺来源商品 / 缺仓库 ——

// TestBundleMemberSourceRequestGuards 请求级的三个明确拒绝（服务端不信任前端）。
func TestBundleMemberSourceRequestGuards(t *testing.T) {
	f := newMemberSrcFixture(t)
	if f == nil {
		return
	}
	bundle := f.mkBundle(t, "守卫套餐", "member-bundle-guards", "GUARD_B", 50.0)

	if got := f.resolveErr(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: "nope",
	}); !strings.Contains(got, productenums.ErrBundleSourceInvalid) {
		t.Fatalf("非法来源应报 %s，实际 %q", productenums.ErrBundleSourceInvalid, got)
	}
	if got := f.resolveErr(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceProduct,
	}); !strings.Contains(got, productenums.ErrBundleSourceProductRequired) {
		t.Fatalf("缺来源商品应报 %s，实际 %q", productenums.ErrBundleSourceProductRequired, got)
	}
	if got := f.resolveErr(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceWarehouse, WarehouseSKUs: []string{"X"},
	}); !strings.Contains(got, productenums.ErrBundleSourceWarehouseRequired) {
		t.Fatalf("缺仓库应报 %s，实际 %q", productenums.ErrBundleSourceWarehouseRequired, got)
	}
	// 自引用：来源商品就是捆绑容器自己 → 请求层面直接拒（不必等保存）。
	if got := f.resolveErr(t, &productdto.ResolveBundleMembersReq{
		ProductID: bundle.ID, Source: productenums.BundleSourceProduct, SourceProductID: bundle.ID,
	}); !strings.Contains(got, productenums.ErrBundleSelfReference) {
		t.Fatalf("自引用应报 %s，实际 %q", productenums.ErrBundleSelfReference, got)
	}
}
