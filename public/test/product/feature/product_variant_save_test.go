// Package feature product 模块 feature 测试 —— 变体清单的「预览—保存」模型（docs/14 §8）。
//
// 用户口径：「变体 sku 也是一样系统生产 + 可编辑，生产只是显示，并不会存入数据库，
// 必须保存才行，所以删除不是删除，是相当于清除前端显示，不存入数据库」。
//
// 本文件按验收逐条覆盖（真实 PostgreSQL + 生产迁移 + 真实 product / inventory service）：
//
//  1. 生成（预览）**不落库**：笛卡尔积只作为待追加的行返回，库里变体一个字节都不变；
//  2. 重复生成不重复追加：库里已有的组合 / 清单已有的组合一律跳过；
//  3. 删除（前端行为）不落库：预览只读，真正删除只发生在「保存」那一步；
//  4. 保存 = 新增清单里库里没有的 + 更新被编辑过的 SKU + 删除清单外的既有变体；
//  5. 清单外要删的既有变体若仍有非零库存或被 BOM 引用 → 跳过该行并逐条回带原因（不整批失败）；
//  6. 服务端不信任前端：非法组合与空 SKU 被拒，SKU 由服务端按变体 SKU 规则重算。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// variantListFixture 隔离 PG schema + 生产迁移/种子 + 真实工程与真实 inventory / product service。
//
// 与生产装配同形的两处接线：SetInventoryService（归属仓解析与库存记录生成）与
// SetInventory（库存 model —— 删变体前的非零库存守卫读的就是它）。**两个都要接**：
// 只接前者时库存守卫会因为读到 nil model 而直接放行，测试会「绿」在一个错的前提上。
type variantListFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	projectID string
	warehouse *inventorydto.WarehouseResp
}

func newVariantListFixture(t *testing.T) *variantListFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	// 内置变动原因字典（迁移 103）必须就位：库存变动只认字典里的原因。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "变体清单测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	products.SetInventory(inventorymodel.NewModel(db))
	inv.SetVariantCost(products)
	wh, err := inv.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SZ", Name: "苏州仓",
	})
	if err != nil {
		t.Fatalf("建仓失败: %v", err)
	}
	return &variantListFixture{
		inventory: inv, products: products, db: db,
		projects: projects, projectID: project.ID, warehouse: wh,
	}
}

// mkAttr 建一个「参与变体」的属性组（值与 key 一一对应，便于断言组合）。
func (f *variantListFixture) mkAttr(t *testing.T, name, key string, valueKeys []string) *productdto.AttributeResp {
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

// mkProduct 建一个引用若干属性组的商品（同时生成它的首个无规格占位变体）。
func (f *variantListFixture) mkProduct(t *testing.T, name, slug string, attributeIDs []string) *productdto.ProductResp {
	t.Helper()
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, AttributeIDs: attributeIDs,
	})
	if err != nil {
		t.Fatalf("创建商品 %s 失败: %v", name, err)
	}
	return res
}

// variantsOf 读商品的变体（按 sort 顺序）。
func (f *variantListFixture) variantsOf(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail.Variants
}

// addStock 走真实库存变动链路入库（原因 = 内置的采购入库，方向 in）。
func (f *variantListFixture) addStock(t *testing.T, v *productdto.VariantResp, qty int) {
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

// previewAll 不勾选任何值（= 全部参与变体的属性组 × 全部启用值）跑一次预览。
func (f *variantListFixture) previewAll(t *testing.T, productID string, existing ...json.RawMessage) *productdto.PreviewVariantResp {
	t.Helper()
	res, err := f.products.PreviewVariantCombinations(context.Background(), &productdto.PreviewVariantReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		ExistingOptionValues: existing,
	})
	if err != nil {
		t.Fatalf("预览组合失败: %v", err)
	}
	return res
}

// save 以清单为准保存（失败即测试失败）。
func (f *variantListFixture) save(t *testing.T, productID string, rows []productdto.VariantListRow) *productdto.SaveVariantListResp {
	t.Helper()
	res, err := f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		OperatorID: "tester", Rows: rows,
	})
	if err != nil {
		t.Fatalf("保存变体清单失败: %v", err)
	}
	return res
}

// saveErr 以清单为准保存并返回错误文案（期望失败时用）。
func (f *variantListFixture) saveErr(t *testing.T, productID string, rows []productdto.VariantListRow) string {
	t.Helper()
	_, err := f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		OperatorID: "tester", Rows: rows,
	})
	if err == nil {
		t.Fatalf("期望被拒，实际保存成功")
	}
	return err.Error()
}

// optionKeyOf 组合的规范化键（与 service 的 optionKey 同一口径：按键排序拼 k=v）。
func optionKeyOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("组合解析失败: %v（%s）", err, string(raw))
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "&")
}

// variantByOption 按组合键找变体（找不到即测试失败）。
func variantByOption(t *testing.T, f *variantListFixture, productID string, raw json.RawMessage) *productdto.VariantResp {
	t.Helper()
	want := optionKeyOf(t, raw)
	for _, v := range f.variantsOf(t, productID) {
		if optionKeyOf(t, v.OptionValues) == want {
			return v
		}
	}
	t.Fatalf("商品 %s 里没有组合 %s 的变体", productID, want)
	return nil
}

// —— 验收 1 / 2 / 3：生成只进清单（不落库）、重复生成不重复追加 ——

// TestVariantPreviewDoesNotPersist 验收 1 + 3：
// 预览把笛卡尔积算出来交给前端，但库里**一个变体都不写**。
func TestVariantPreviewDoesNotPersist(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "衬衫", "shirt-preview", []string{color.ID, size.ID})

	before := f.variantsOf(t, p.ID)
	if len(before) != 1 {
		t.Fatalf("前置条件：商品创建后应有 1 个无规格占位变体，实际 %d", len(before))
	}
	res := f.previewAll(t, p.ID)
	if res.Total != 4 || len(res.Rows) != 4 || res.Skipped != 0 {
		t.Fatalf("2×2 应返回 4 行待追加，实际 total=%d rows=%d skipped=%d",
			res.Total, len(res.Rows), res.Skipped)
	}
	seen := map[string]bool{}
	for _, row := range res.Rows {
		if !strings.HasSuffix(row.SKUCode, "_V") {
			t.Fatalf("系统生成的变体 SKU 应以 _V 结尾，实际 %q", row.SKUCode)
		}
		if seen[row.SKUCode] {
			t.Fatalf("预览行 SKU 重复：%s", row.SKUCode)
		}
		seen[row.SKUCode] = true
		if strings.TrimSpace(row.OptionKey) == "" {
			t.Fatalf("预览行必须带组合键（前端据此去重）")
		}
	}
	// 生成不落库：库里还是那一个占位变体，且它的 SKU 一字未改。
	after := f.variantsOf(t, p.ID)
	if len(after) != 1 {
		t.Fatalf("预览不该写库：期望仍是 1 个变体，实际 %d", len(after))
	}
	if after[0].SKUCode != before[0].SKUCode {
		t.Fatalf("预览不该改库里的 SKU：%s → %s", before[0].SKUCode, after[0].SKUCode)
	}
}

// TestVariantPreviewSkipsExistingAndReappears 验收 2 + 3：
// 库里已有的组合不会被再次追加；把某一行从清单里删掉（不落库）后再点生成，它才会回来。
func TestVariantPreviewSkipsExistingAndReappears(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "外套", "coat-preview", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)
	rows := make([]productdto.VariantListRow, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListRow{OptionValues: row.OptionValues})
	}
	if res := f.save(t, p.ID, rows); res.Created != 4 {
		t.Fatalf("首次保存应新增 4 个组合，实际 %d", res.Created)
	}
	stored := f.variantsOf(t, p.ID)
	if len(stored) != 4 {
		t.Fatalf("保存后应有 4 个变体（无规格占位变体已随清单外删除），实际 %d", len(stored))
	}

	// 清单里已有这 4 行：再点生成不再追加（库里也没有第 5 个）。
	existing := make([]json.RawMessage, 0, len(stored))
	for _, v := range stored {
		existing = append(existing, v.OptionValues)
	}
	again := f.previewAll(t, p.ID, existing...)
	if len(again.Rows) != 0 || again.Skipped != 4 {
		t.Fatalf("重复生成不该追加：rows=%d skipped=%d", len(again.Rows), again.Skipped)
	}
	// 库里已有的组合同样跳过（即便前端没回传清单）。
	if res := f.previewAll(t, p.ID); len(res.Rows) != 0 || res.Skipped != 4 {
		t.Fatalf("库里已有的组合应跳过：rows=%d skipped=%d", len(res.Rows), res.Skipped)
	}

	// 把其中一行移出清单（= 前端删除）并保存：库里少一个组合。
	dropped := stored[0]
	keep := make([]productdto.VariantListRow, 0, len(stored)-1)
	for _, v := range stored[1:] {
		keep = append(keep, productdto.VariantListRow{VariantID: v.ID, SKUCode: v.SKUCode, OptionValues: v.OptionValues})
	}
	if res := f.save(t, p.ID, keep); res.Deleted != 1 {
		t.Fatalf("清单外的那一行应被删除，实际 deleted=%d", res.Deleted)
	}
	// 再点生成：被删掉的组合回来了（说明「生成」是显式动作，不会自己冒出来）。
	back := f.previewAll(t, p.ID)
	if len(back.Rows) != 1 {
		t.Fatalf("删掉的那一个组合应能被重新生成，实际 rows=%d", len(back.Rows))
	}
	if optionKeyOf(t, back.Rows[0].OptionValues) != optionKeyOf(t, dropped.OptionValues) {
		t.Fatalf("重新生成的组合应与被删的那个一致")
	}
}

// —— 验收 4：保存 = 新增 + 改 SKU + 删清单外 ——

// TestVariantSaveAddsUpdatesAndDeletes 一次保存同时做完三件事。
func TestVariantSaveAddsUpdatesAndDeletes(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "卫衣", "hoodie-save", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)
	// 第一段：只保留前两个组合。
	first := []productdto.VariantListRow{
		{OptionValues: pv.Rows[0].OptionValues},
		{OptionValues: pv.Rows[1].OptionValues},
	}
	if res := f.save(t, p.ID, first); res.Created != 2 || res.Deleted != 1 {
		t.Fatalf("首次保存应新增 2 个、删掉清单外的占位变体：created=%d deleted=%d", res.Created, res.Deleted)
	}

	// 第二段：保留第一行但改 SKU；丢弃第二行；补上另外两个组合。
	v1 := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	custom := "SZ_CUSTOM_RED_S_V"
	second := []productdto.VariantListRow{
		{VariantID: v1.ID, SKUCode: custom, OptionValues: pv.Rows[0].OptionValues},
		{OptionValues: pv.Rows[2].OptionValues},
		{OptionValues: pv.Rows[3].OptionValues},
	}
	res := f.save(t, p.ID, second)
	if res.Created != 2 || res.Updated != 1 || res.Deleted != 1 {
		t.Fatalf("应新增 2 / 改 1 / 删 1，实际 created=%d updated=%d deleted=%d",
			res.Created, res.Updated, res.Deleted)
	}
	after := f.variantsOf(t, p.ID)
	if len(after) != 3 {
		t.Fatalf("最终应有 3 个变体，实际 %d", len(after))
	}
	kept := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	if kept.SKUCode != custom {
		t.Fatalf("被编辑的 SKU 应落库（%s），实际 %q", custom, kept.SKUCode)
	}
	if kept.ID != v1.ID {
		t.Fatalf("改 SKU 应改同一条变体（不是删旧建新），实际 id %s → %s", v1.ID, kept.ID)
	}
	// 丢弃的那一行确实没了。
	for _, v := range after {
		if optionKeyOf(t, v.OptionValues) == optionKeyOf(t, pv.Rows[1].OptionValues) {
			t.Fatalf("清单外的那一行应被删除")
		}
	}
}

// —— 验收 5：有库存 / 被引用时跳过该行，且不整批失败 ——

// TestVariantSaveSkipsStockAndBOM 清单外要删的行：有非零库存或被 BOM 引用 → 跳过并逐条回带原因。
func TestVariantSaveSkipsStockAndBOM(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "夹克", "jacket-skip", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)
	rows := make([]productdto.VariantListRow, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListRow{OptionValues: row.OptionValues})
	}
	f.save(t, p.ID, rows)

	withStock := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	referenced := variantByOption(t, f, p.ID, pv.Rows[1].OptionValues)
	parent := variantByOption(t, f, p.ID, pv.Rows[2].OptionValues)
	kept := variantByOption(t, f, p.ID, pv.Rows[3].OptionValues)
	f.addStock(t, withStock, 3)
	if _, err := f.inventory.SetBOM(context.Background(), &inventorydto.SetBOMReq{
		ProjectID: f.projectID, ParentVariantID: parent.ID, ParentSKUCode: parent.SKUCode,
		Items: []inventorydto.BOMItemReq{{
			ComponentVariantID: referenced.ID, ComponentSKUCode: referenced.SKUCode, Quantity: 1,
		}},
	}); err != nil {
		t.Fatalf("配置 BOM 失败: %v", err)
	}

	// 清单只留一个：其余三个都在删除范围内（两个被守卫拦下，一个真的删掉）。
	res := f.save(t, p.ID, []productdto.VariantListRow{{
		VariantID: kept.ID, SKUCode: kept.SKUCode, OptionValues: kept.OptionValues,
	}})
	if res.Deleted != 1 {
		t.Fatalf("无库存无引用的那一行应被删除，实际 deleted=%d", res.Deleted)
	}
	if len(res.Skipped) != 2 {
		t.Fatalf("应有 2 行被跳过（库存 / BOM），实际 %d：%+v", len(res.Skipped), res.Skipped)
	}
	reasons := map[string]bool{}
	for _, skip := range res.Skipped {
		reasons[skip.Reason] = true
	}
	if !reasons[productenums.VariantSkipHasStock] {
		t.Fatalf("有库存的那一行应报 %s，实际 %+v", productenums.VariantSkipHasStock, res.Skipped)
	}
	if !reasons[productenums.VariantSkipReferenced] {
		t.Fatalf("被 BOM 引用的那一行应报 %s，实际 %+v", productenums.VariantSkipReferenced, res.Skipped)
	}
	// 不整批失败：被拦下的两个变体仍在库里，没有被误删。
	after := f.variantsOf(t, p.ID)
	ids := map[string]bool{}
	for _, v := range after {
		ids[v.ID] = true
	}
	if !ids[withStock.ID] || !ids[referenced.ID] || !ids[kept.ID] {
		t.Fatalf("被跳过的变体必须留在库里：%+v", ids)
	}
	if ids[parent.ID] {
		t.Fatalf("清单外且没有引用的变体应被删除")
	}
}

// —— 验收 6：服务端不信任前端 ——

// TestVariantSaveRejectsIllegalCombinationAndEmptySKU 非法组合与空 SKU 都被明确拒绝。
func TestVariantSaveRejectsIllegalCombinationAndEmptySKU(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "长裤", "pants-guard", []string{color.ID, size.ID})

	// 属性组不在该商品上（前端塞了一个别的组）。
	msg := f.saveErr(t, p.ID, []productdto.VariantListRow{{
		OptionValues: json.RawMessage(`{"nope":"x"}`),
	}})
	if !strings.Contains(msg, productenums.ErrVariationAttributeInvalid) {
		t.Fatalf("非法属性组应报 %s，实际 %q", productenums.ErrVariationAttributeInvalid, msg)
	}
	// 取值不在该属性组内（停用 / 不存在的值）。
	msg = f.saveErr(t, p.ID, []productdto.VariantListRow{{
		OptionValues: json.RawMessage(`{"color":"purple","size":"s"}`),
	}})
	if !strings.Contains(msg, productenums.ErrVariationValueInvalid) {
		t.Fatalf("非法取值应报 %s，实际 %q", productenums.ErrVariationValueInvalid, msg)
	}
	// 新增行没有规格组合：空组合不是「无规格变体」的入口（占位变体只由商品创建产生）。
	msg = f.saveErr(t, p.ID, []productdto.VariantListRow{{OptionValues: json.RawMessage(`{}`)}})
	if !strings.Contains(msg, productenums.ErrVariantOptionsInvalid) {
		t.Fatalf("空组合应报 %s，实际 %q", productenums.ErrVariantOptionsInvalid, msg)
	}

	// 新增行没填 SKU：由系统按变体 SKU 规则生成（不是错误）。
	pv := f.previewAll(t, p.ID)
	saved := f.save(t, p.ID, []productdto.VariantListRow{{OptionValues: pv.Rows[0].OptionValues}})
	if saved.Created != 1 {
		t.Fatalf("新增行应落库 1 个变体，实际 %d", saved.Created)
	}

	// 既有行把 SKU 清空（含只填了空段）：明确报错，而不是静默补一个系统编码。
	stored := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	msg = f.saveErr(t, p.ID, []productdto.VariantListRow{{
		VariantID: stored.ID, SKUCode: "   ", OptionValues: stored.OptionValues,
	}})
	if !strings.Contains(msg, productenums.ErrVariantSKUEmpty) {
		t.Fatalf("空 SKU 应报 %s，实际 %q", productenums.ErrVariantSKUEmpty, msg)
	}
	msg = f.saveErr(t, p.ID, []productdto.VariantListRow{{
		VariantID: stored.ID, SKUCode: "SZ__RED_V", OptionValues: stored.OptionValues,
	}})
	if !strings.Contains(msg, productenums.ErrVariantSKUEmpty) {
		t.Fatalf("规范化后为空段应报 %s，实际 %q", productenums.ErrVariantSKUEmpty, msg)
	}
	// 新增行直接抄库里的 SKU：唯一性按 product_id + sku_code 判，明确拒绝（前端提交的值不作数）。
	msg = f.saveErr(t, p.ID, []productdto.VariantListRow{{
		SKUCode: stored.SKUCode, OptionValues: pv.Rows[1].OptionValues,
	}})
	if !strings.Contains(msg, productenums.ErrSkuTaken) {
		t.Fatalf("抄用已占用的 SKU 应报 %s，实际 %q", productenums.ErrSkuTaken, msg)
	}
	// 同一个变体在清单里出现两次：第二行按「跳过」处理并回带原因（不整批失败、不重复写）。
	dup := f.save(t, p.ID, []productdto.VariantListRow{{
		VariantID: stored.ID, SKUCode: stored.SKUCode, OptionValues: stored.OptionValues,
	}, {
		VariantID: stored.ID, SKUCode: stored.SKUCode, OptionValues: stored.OptionValues,
	}})
	dupSkipped := false
	for _, skip := range dup.Skipped {
		if skip.Reason == productenums.VariantSkipDuplicated {
			dupSkipped = true
		}
	}
	if !dupSkipped {
		t.Fatalf("同一组合重复出现时应记一条 %s，实际 %+v", productenums.VariantSkipDuplicated, dup.Skipped)
	}
	if dup.Created != 0 || dup.Updated != 0 || dup.Deleted != 0 {
		t.Fatalf("重复行不该产生任何写入：created=%d updated=%d deleted=%d",
			dup.Created, dup.Updated, dup.Deleted)
	}
}

// —— 页面链路：预览回 JSON、保存回详情页带结论 ——

// TestVariantListPagePreviewAndSaveFlow 后台页面的两个端点在真实链路里可用。
func TestVariantListPagePreviewAndSaveFlow(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "围巾", "scarf-page", []string{color.ID, size.ID})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.POST("/admin/products/variant/preview", handle.ProductsVariantPreview)
	engine.POST("/admin/products/variant/save", handle.ProductsVariantSave)

	rec := postForm(engine, "/admin/products/variant/preview", url.Values{
		"projectId": {f.projectID}, "productId": {p.ID}, "warehouseId": {f.warehouse.ID},
		"attr:" + color.ID: {color.Values[0].ID},
		"attr:" + size.ID:  {size.Values[0].ID, size.Values[1].ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var preview struct {
		OK   bool `json:"ok"`
		Rows []struct {
			SKUCode      string          `json:"skuCode"`
			OptionValues json.RawMessage `json:"optionValues"`
			Spec         string          `json:"spec"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("预览响应不是合法 JSON: %v（%s）", err, rec.Body.String())
	}
	if !preview.OK || len(preview.Rows) != 2 {
		t.Fatalf("勾选 1×2 应返回 2 行，实际 ok=%v rows=%d", preview.OK, len(preview.Rows))
	}
	for _, row := range preview.Rows {
		if row.SKUCode == "" || row.Spec == "" || len(row.OptionValues) == 0 {
			t.Fatalf("预览行必须带 SKU / 规格文本 / 组合：%+v", row)
		}
	}
	// 预览不落库（页面链路同样如此）。
	if got := f.variantsOf(t, p.ID); len(got) != 1 {
		t.Fatalf("预览不该写库：期望 1 个变体，实际 %d", len(got))
	}

	rowsJSON, merr := json.Marshal([]map[string]any{
		{"variantId": "", "skuCode": preview.Rows[0].SKUCode, "optionValues": json.RawMessage(preview.Rows[0].OptionValues)},
		{"variantId": "", "skuCode": preview.Rows[1].SKUCode, "optionValues": json.RawMessage(preview.Rows[1].OptionValues)},
	})
	if merr != nil {
		t.Fatalf("组装清单失败: %v", merr)
	}
	rec = postForm(engine, "/admin/products/variant/save", url.Values{
		"projectId": {f.projectID}, "productId": {p.ID}, "warehouseId": {f.warehouse.ID},
		"rows": {string(rowsJSON)},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("保存应 302 回编辑页，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "done=") {
		t.Fatalf("保存结果应经 ?done= 回带（页面上可见），实际 Location=%q", loc)
	}
	if !strings.Contains(loc, url.QueryEscape("新增 2 个")) {
		t.Fatalf("回带文案应含新增计数，实际 Location=%q", loc)
	}
	if got := f.variantsOf(t, p.ID); len(got) != 2 {
		t.Fatalf("保存后应有 2 个变体，实际 %d", len(got))
	}
}

// 保证 httptest 被使用（其余用例走 service 直调）。
var _ = httptest.NewRecorder

// TestVariantListSeedFromStoredVariants 验收 1：
// 详情页清单的初始行 = 库里已有的变体（带 SKU 与 option_values），**不是**重算笛卡尔积 ——
// 已预览但没保存的组合不会自己出现在清单里（这正是「删了又回来」的根因）。
func TestVariantListSeedFromStoredVariants(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "帽子", "hat-seed", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)
	if len(pv.Rows) != 4 {
		t.Fatalf("前置条件：2×2 应有 4 个组合，实际 %d", len(pv.Rows))
	}
	// 只保存前两个组合：后两个仍是「已预览、未保存」。
	f.save(t, p.ID, []productdto.VariantListRow{
		{OptionValues: pv.Rows[0].OptionValues},
		{OptionValues: pv.Rows[1].OptionValues},
	})
	stored := f.variantsOf(t, p.ID)
	if len(stored) != 2 {
		t.Fatalf("前置条件：库里应只有 2 个组合，实际 %d", len(stored))
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.GET("/admin/products/edit", handle.ProductEditPage)
	rec := httptest.NewRecorder()
	// 变体清单在**编辑页**（详情页只读）：清单的初始行 / 保存入口都在那里。
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/edit?project="+f.projectID+"&product="+p.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("编辑页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, v := range stored {
		if !strings.Contains(body, `value="`+v.SKUCode+`"`) {
			t.Fatalf("清单初始行应带库里的 SKU %q", v.SKUCode)
		}
		if !strings.Contains(body, `data-variant-id="`+v.ID+`"`) {
			t.Fatalf("清单初始行应带既有变体 id %q（保存时据此更新而不是新建）", v.ID)
		}
	}
	// 未保存的两个组合：它们的系统建议 SKU 不该出现在清单里。
	for _, row := range pv.Rows[2:] {
		if strings.Contains(body, row.SKUCode) {
			t.Fatalf("未保存的组合出现在清单里（说明清单重算了笛卡尔积）：%s", row.SKUCode)
		}
	}
	if !strings.Contains(body, "保存变体清单") {
		t.Fatalf("清单区块应带保存入口")
	}
}
