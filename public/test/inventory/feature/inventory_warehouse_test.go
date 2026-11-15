package feature

import (
	"bytes"
	"context"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/html"
	"gorm.io/gorm"

	"go_wp/internal/middleware/adminctx"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	inventorymodel "go_wp/internal/module/inventory/model"
	purchasestock "go_wp/internal/module/inventory/outbound/purchasestock"
	inventoryservice "go_wp/internal/module/inventory/service"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	purchasemodel "go_wp/internal/module/purchase/model"
	purchaseservice "go_wp/internal/module/purchase/service"
	"go_wp/internal/templates"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

type invFixture struct {
	inventory *inventoryservice.Service
	purchase  *purchaseservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	projectID string
}

// injectInventoryFeatureWords 把本用例库里的词条灌进 i18n 缓存。
func injectInventoryFeatureWords(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		ItemKey string `gorm:"column:item_key"`
		Lang    string
		Value   string `gorm:"column:value"`
	}
	if err := db.Table("sys_i18n").Select("item_key", "lang", "item_value AS value").
		Where("status = ?", 1).Scan(&rows).Error; err != nil {
		t.Fatalf("读取词条失败：%v", err)
	}
	words := make(map[string]map[string]string, len(rows))
	for _, r := range rows {
		if words[r.ItemKey] == nil {
			words[r.ItemKey] = map[string]string{}
		}
		words[r.ItemKey][r.Lang] = r.Value
	}
	i18n.InjectForTest(words, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

func newInvFixture(t *testing.T) *invFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}

	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	// 失败区与结果片段只从词库取词：把本用例库里的词条灌进缓存，不读会漂移的全局库。
	injectInventoryFeatureWords(t, db)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "库存测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)

	purchase := purchaseservice.NewService(purchasemodel.NewModel(db), projects)
	purchase.SetStock(purchasestock.New(inv, inv, inv))
	purchase.SetVariantCost(products)
	return &invFixture{
		inventory: inv, purchase: purchase, products: products, db: db,
		projects: projects, projectID: project.ID,
	}
}

func (f *invFixture) createWarehouse(t *testing.T, code, name string, isDefault bool) *inventorydto.WarehouseResp {
	t.Helper()
	w, err := f.inventory.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: code, Name: name, IsDefault: isDefault,
	})
	if err != nil {
		t.Fatalf("建仓 %s 失败: %v", code, err)
	}
	return w
}

func (f *invFixture) firstVariant(t *testing.T, productID string) *productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if len(detail.Variants) == 0 {
		t.Fatalf("商品 %s 没有任何变体", productID)
	}
	return detail.Variants[0]
}

func bareSKU(code, warehouseCode string) string {
	return productservice.StripWarehousePrefix(code, warehouseCode)
}

func (f *invFixture) stockQty(t *testing.T, variantID, warehouseID string) int {
	t.Helper()
	var qty int
	err := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&qty).Error
	if err != nil {
		t.Fatalf("读库存真源失败: %v", err)
	}
	return qty
}

func TestWarehouseCreateAndDefaultFallback(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	sz := f.createWarehouse(t, "SZ", "苏州仓", false)
	if !sz.IsDefault {
		t.Fatalf("工程内第一个仓应自动成为默认仓，实际 isDefault=false")
	}

	sh := f.createWarehouse(t, "sh", "上海仓", false)
	if sh.Code != "SH" {
		t.Fatalf("短码应归一为大写，实际 %q", sh.Code)
	}
	if sh.IsDefault {
		t.Fatalf("第二个仓不应自动成为默认仓")
	}

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)
	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sz.ID})
	if err != nil {
		t.Fatalf("默认仓兜底未生成库存记录: %v", err)
	}
	if row.Quantity != 0 {
		t.Fatalf("新生成的库存记录应为 0，实际 %d", row.Quantity)
	}

	yes := true
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{ID: sh.ID, IsDefault: &yes}); err != nil {
		t.Fatalf("切换默认仓失败: %v", err)
	}
	list, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("仓库列表失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 个仓库，实际 %d", len(list))
	}
	if !list[0].IsDefault || list[0].ID != sh.ID {
		t.Fatalf("默认仓应排在列表最前且为 SH，实际 %+v", list[0])
	}
	for _, w := range list {
		if w.ID == sz.ID && w.IsDefault {
			t.Fatalf("切换默认仓后 SZ 不应再是默认仓")
		}
	}

	v2, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if _, err = f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v2.ID, WarehouseID: sh.ID}); err != nil {
		t.Fatalf("切换默认仓后应兜底到 SH: %v", err)
	}
	if _, err = f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v2.ID, WarehouseID: sz.ID}); err == nil {
		t.Fatalf("变体只应落在归属仓，SZ 不应有它的库存记录")
	}

	if err = f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{ID: sh.ID}); err == nil ||
		err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("删除默认仓应返回 ErrWarehouseIsDefault，实际 %v", err)
	}
	disabled := inventoryenums.StatusDisabled
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{ID: sh.ID, Status: &disabled}); err == nil ||
		err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("停用默认仓应返回 ErrWarehouseIsDefault，实际 %v", err)
	}
	no := false
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{ID: sh.ID, IsDefault: &no}); err == nil ||
		err.Error() != inventoryenums.ErrWarehouseIsDefault {
		t.Fatalf("取消默认标记应返回 ErrWarehouseIsDefault，实际 %v", err)
	}

	if err = f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{ID: sz.ID}); err != nil {
		t.Fatalf("删除非默认仓失败: %v", err)
	}

	if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: "sh", Name: "重复短码",
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseCodeTaken {
		t.Fatalf("重复短码应返回 ErrWarehouseCodeTaken，实际 %v", err)
	}
	for _, bad := range []string{"S_1", "苏州", "TOOLONGCODE"} {
		if _, err = f.inventory.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
			ProjectID: f.projectID, Code: bad, Name: "非法短码",
		}); err == nil || err.Error() != inventoryenums.ErrWarehouseCodeInvalid {
			t.Fatalf("非法短码 %q 应返回 ErrWarehouseCodeInvalid，实际 %v", bad, err)
		}
	}

	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "没有仓库的工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}
	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: other.ID, VariantID: v.ID,
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseDefaultMissing {
		t.Fatalf("缺默认仓应返回 ErrWarehouseDefaultMissing，实际 %v", err)
	}
}

func TestVariantCreateGeneratesStockRow(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v1 := f.firstVariant(t, p.ID)
	if !strings.HasPrefix(v1.SKUCode, "SZ_TEE") {
		t.Fatalf("SKU 编码应以归属仓短码开头（SZ_TEE…），实际 %q", v1.SKUCode)
	}
	if got := f.stockQty(t, v1.ID, sz.ID); got != 0 {
		t.Fatalf("首个变体应在默认仓有一条 0 库存记录，实际 %d", got)
	}

	v2, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, WarehouseID: sh.ID})
	if err != nil {
		t.Fatalf("指定仓库新增变体失败: %v", err)
	}
	if !strings.HasPrefix(v2.SKUCode, "SZ_TEE") {
		t.Fatalf("变体 SKU 应沿用商品容器主体前缀（SZ_TEE…），实际 %q", v2.SKUCode)
	}
	if strings.HasPrefix(v2.SKUCode, "SH_") {
		t.Fatalf("新增变体的仓库选择不应改写 SKU 编码，实际 %q", v2.SKUCode)
	}
	if got := f.stockQty(t, v2.ID, sh.ID); got != 0 {
		t.Fatalf("变体应在指定仓有一条 0 库存记录，实际 %d", got)
	}
	var n int64
	if err = f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", v2.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("变体应恰好有一条库存记录，实际 %d", n)
	}

	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "尺寸", Key: "size",
		Values: []productdto.AttributeValueReq{{Label: "S", Key: "s"}, {Label: "M", Key: "m"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{ID: p.ID, AttributeIDs: []string{attr.ID}}); err != nil {
		t.Fatalf("给商品挂属性组失败: %v", err)
	}

	beforeSKUs := map[string]bool{}
	if detail, derr := f.products.Get(ctx, &productdto.GetReq{ID: p.ID}); derr == nil {
		for _, v := range detail.Variants {
			beforeSKUs[v.SKUCode] = true
		}
	}
	gen, err := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{
		ProductID: p.ID, WarehouseID: sh.ID,
	})
	if err != nil {
		t.Fatalf("生成变体组合失败: %v", err)
	}

	if gen.Created+gen.Adopted != 2 {
		t.Fatalf("应覆盖 2 个组合（新建 + 承接），实际 created=%d adopted=%d", gen.Created, gen.Adopted)
	}
	if gen.Created == 0 {
		t.Fatalf("至少应新建 1 个组合变体，实际 %d", gen.Created)
	}
	seen := map[string]bool{}
	newOnes := 0
	for _, v := range gen.Variants {
		if seen[v.SKUCode] {
			t.Fatalf("SKU 编码重复：%s", v.SKUCode)
		}
		seen[v.SKUCode] = true
		if beforeSKUs[v.SKUCode] {

			var cnt int64
			if qerr := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", v.ID).Scan(&cnt).Error; qerr != nil {
				t.Fatalf("统计既有变体库存行失败: %v", qerr)
			}
			if cnt != 1 {
				t.Fatalf("既有变体 %s 的库存记录数不应变化，实际 %d", v.SKUCode, cnt)
			}
			continue
		}
		if !strings.HasPrefix(v.SKUCode, "SZ_TEE") {
			t.Fatalf("生成路径的变体 SKU 应沿用容器主体前缀（SZ_TEE…），实际 %q", v.SKUCode)
		}
		newOnes++
		if _, gerr := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sh.ID}); gerr != nil {
			t.Fatalf("生成的变体 %s 缺少归属仓库存记录: %v", v.SKUCode, gerr)
		}
	}
	if newOnes != gen.Created {
		t.Fatalf("指定仓库生成的新变体应全部落在该仓：new=%d created=%d", newOnes, gen.Created)
	}

	if _, err = f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: p.ID, WarehouseID: "00000000-0000-0000-0000-000000000000",
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseNotFound {
		t.Fatalf("不存在的仓库应返回 ErrWarehouseNotFound，实际 %v", err)
	}
	before := countVariants(t, f, p.ID)
	mismatch := f.createWarehouse(t, "BJ", "北京仓", false)
	if _, err = f.inventory.UpdateWarehouse(ctx, &inventorydto.UpdateWarehouseReq{
		ID: mismatch.ID, Status: strPtr(inventoryenums.StatusDisabled),
	}); err != nil {
		t.Fatalf("停用仓库失败: %v", err)
	}
	if _, err = f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: p.ID, WarehouseID: mismatch.ID,
	}); err == nil || err.Error() != inventoryenums.ErrWarehouseDisabled {
		t.Fatalf("停用仓库应返回 ErrWarehouseDisabled，实际 %v", err)
	}
	if after := countVariants(t, f, p.ID); after != before {
		t.Fatalf("归属仓解析失败时不应落库变体：前 %d 后 %d", before, after)
	}
}

func TestStockPerSkuPerWarehouse(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)

	bare := bareSKU(v.SKUCode, sz.Code)

	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, ProductID: p.ID, VariantID: v.ID, SKUCode: bare, WarehouseID: sh.ID,
	}); err != nil {
		t.Fatalf("在第二个仓生成库存记录失败: %v", err)
	}

	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, ProductID: p.ID, VariantID: v.ID, SKUCode: bare, WarehouseID: sh.ID,
	}); err != nil {
		t.Fatalf("重复生成库存记录失败: %v", err)
	}
	var n int64
	if err = f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ?", v.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("同一 SKU 应恰好有两行（SZ + SH），实际 %d", n)
	}

	if err = f.db.Exec("INSERT INTO inventory_stocks (project_id, warehouse_id, product_id, variant_id, sku_code) VALUES (?,?,?,?,?)",
		f.projectID, sh.ID, p.ID, v.ID, bare).Error; err == nil {
		t.Fatalf("(变体, 仓库) 重复插入应被唯一约束拒绝")
	}

	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: bare,
	})
	if err != nil {
		t.Fatalf("按 SKU 查库存失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("某 SKU 应有 2 条仓库维度记录，实际 %d", len(rows))
	}
	if rows[0].WarehouseID != sz.ID || rows[1].WarehouseID != sh.ID {
		t.Fatalf("默认仓应排在最前：%+v", rows)
	}
	if rows[0].WarehouseName == "" || rows[1].WarehouseCode != "SH" {
		t.Fatalf("库存行应带上仓库名与短码：%+v", rows)
	}
	for _, r := range rows {
		if r.Quantity != 0 {
			t.Fatalf("初始库存应为 0，实际 %d", r.Quantity)
		}
		if r.SKUCode != bare || r.VariantID != v.ID {
			t.Fatalf("库存维度应指向同一仓库侧 SKU（裸码 %q）/ 变体：%+v", bare, r)
		}
	}
}

func TestStockReadsTrueSourceNotCache(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)

	row, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sz.ID})
	if err != nil {
		t.Fatalf("读库存失败: %v", err)
	}
	if row.Quantity != 0 {
		t.Fatalf("库存必须读真源（0），实际读了缓存？得到 %d", row.Quantity)
	}

	bare := bareSKU(v.SKUCode, sz.Code)
	rows, err := f.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
		ProjectID: f.projectID, SKUCode: bare,
	})
	if err != nil {
		t.Fatalf("按 SKU 查库存失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Quantity != 0 {
		t.Fatalf("列表也必须读真源，实际 %+v", rows)
	}

}

func TestInventoryAdminPage(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()

	rec := postForm(engine, "/admin/inventory/warehouse/create", url.Values{
		"projectId": {f.projectID}, "code": {"SZ"}, "name": {"苏州仓"}, "isDefault": {"1"},
	})
	assertInventoryJump(t, rec, "ok")
	rec = postForm(engine, "/admin/inventory/warehouse/create", url.Values{
		"projectId": {f.projectID}, "code": {"SH"}, "name": {"上海仓"},
	})
	assertInventoryJump(t, rec, "ok")

	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v := f.firstVariant(t, p.ID)
	warehouses, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil || len(warehouses) != 2 {
		t.Fatalf("仓库列表异常：%v %+v", err, warehouses)
	}
	shID := ""
	for _, w := range warehouses {
		if w.Code == "SH" {
			shID = w.ID
		}
	}
	if _, err = f.inventory.EnsureStock(ctx, &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode, WarehouseID: shID,
	}); err != nil {
		t.Fatalf("在第二个仓生成库存记录失败: %v", err)
	}

	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID+"&sku="+url.QueryEscape(v.SKUCode))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		"库存管理", "苏州仓", "上海仓", "SZ", "默认仓",
		v.SKUCode, "某 SKU 的各仓库存", `class="table-wrap"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("库存页缺少 %q", want)
		}
	}

	rec = httptestGet(engine, "/admin/inventory/warehouses?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("仓库管理页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	whBody := rec.Body.String()
	for _, want := range []string{"仓库管理", "新建仓库", "苏州仓", "上海仓", "SZ", "默认仓"} {
		if !strings.Contains(whBody, want) {
			t.Fatalf("仓库管理页缺少 %q", want)
		}
	}

	if strings.Count(body, v.SKUCode) < 3 {
		t.Fatalf("页面应同时列出该 SKU 在两个仓的库存行：%s", body)
	}

	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("无 SKU 查询时页面应 200，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "选一个 SKU 后") {
		t.Fatalf("无 SKU 时应给出查询提示")
	}

	rec = postForm(engine, "/admin/inventory/warehouse/default", url.Values{
		"projectId": {f.projectID}, "id": {shID},
	})
	assertInventoryJump(t, rec, "ok")
	after, err := f.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: f.projectID})
	if err != nil || !after[0].IsDefault || after[0].ID != shID {
		t.Fatalf("设默认仓未生效：%v %+v", err, after)
	}

	rec = postForm(engine, "/admin/inventory/warehouse/delete", url.Values{
		"projectId": {f.projectID}, "id": {shID},
	})
	assertInventoryJump(t, rec, "err")
}

func TestProductPageWarehouseSelect(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "Tee", Slug: "tee"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们页面渲染为空。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"product:create": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"product.create": true})
	})
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", pageview.Slot(producthttp.ProductListPageTemplate, pageview.RenderPage), handle.ProductsPage)
	engine.GET("/admin/products/new", pageview.Slot(producthttp.ProductNewPageTemplate, pageview.RenderPage), handle.ProductNewPage)

	engine.POST("/api/product/variant/create", pageview.HTMLFragment, producthttp.NewProductHandle(f.products).CreateVariant)

	rec := httptestGet(engine, "/admin/products?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("商品列表页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	listBody := rec.Body.String()
	if !strings.Contains(listBody, `href="/admin/products/new?project=`+f.projectID+`"`) {
		t.Fatalf("商品列表缺少保留当前工程的独立新建页入口")
	}
	if strings.Contains(listBody, `name="warehouseIds"`) {
		t.Fatalf("商品列表不应再承载新建商品的多仓表单")
	}

	rec = httptestGet(engine, "/admin/products/new?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("商品新建页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	idAt := strings.Index(body, `id="product-create-form"`)
	if idAt < 0 {
		t.Fatalf("商品新建页缺少创建表单")
	}
	formStart := strings.LastIndex(body[:idAt], "<form")
	if formStart < 0 {
		t.Fatalf("商品新建页缺少创建表单")
	}
	formEnd := strings.Index(body[formStart:], `</form>`)
	if formEnd < 0 {
		t.Fatalf("商品新建表单缺少结束标签")
	}
	form := body[formStart : formStart+formEnd]

	for _, want := range []string{
		"建在哪些仓（可多选，不勾即默认仓）",
		`name="warehouseIds" value="` + sz.ID + `"`,
		`name="warehouseIds" value="` + sh.ID + `"`,
		"苏州仓（SZ） · 默认仓",
		"上海仓（SH）",
		"按本表顺序第一个勾中的仓是认领仓",
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("商品新建表单缺少归属仓（多仓勾选）内容 %q", want)
		}
	}
	if strings.Index(form, `name="warehouseIds" value="`+sz.ID+`"`) >=
		strings.Index(form, `name="warehouseIds" value="`+sh.ID+`"`) {
		t.Fatalf("默认仓选项应在非默认仓之前")
	}

	before := countVariants(t, f, p.ID)
	rec = postForm(engine, "/api/product/variant/create", url.Values{
		"projectId": {f.projectID}, "productId": {p.ID}, "warehouseId": {sh.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("新建变体应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("新建变体应回 HTML 片段，Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("新建变体应回成功片段：%s", rec.Body.String())
	}
	if after := countVariants(t, f, p.ID); after != before+1 {
		t.Fatalf("变体应真实入库：POST 前 %d 个，POST 后 %d 个", before, after)
	}
	detail, err := f.products.Get(ctx, &productdto.GetReq{ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}

	var picked *productdto.VariantResp
	for _, v := range detail.Variants {
		if got := f.stockQty(t, v.ID, sh.ID); got == 0 {
			if _, gerr := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sh.ID}); gerr == nil {
				picked = v
			}
		}
	}
	if picked == nil {
		t.Fatalf("表单指定仓库后应在该仓生成库存记录，实际 %+v", detail.Variants)
	}

	inChosen := 0
	for _, v := range detail.Variants {
		if _, gerr := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: v.ID, WarehouseID: sh.ID}); gerr == nil {
			inChosen++
		}
	}
	if inChosen != 1 {
		t.Fatalf("所选仓应恰好多出一条库存记录，实际 %d 条（变体共 %d 个）", inChosen, len(detail.Variants))
	}
	if !strings.HasPrefix(picked.SKUCode, "SZ_TEE") {
		t.Fatalf("表单路径的变体 SKU 也应沿用容器主体前缀（SZ_TEE…），实际 %q", picked.SKUCode)
	}
	if got := f.stockQty(t, picked.ID, sh.ID); got != 0 {
		t.Fatalf("表单路径也应在归属仓生成 0 库存记录，实际 %d", got)
	}
}

func TestInventoryPermissionsAndMenusSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	if err := schema.Apply(f.db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE module = 'inventory'").Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}

	// 这条钉的是「库存域权限点被完整 seed」：数量会随后续迁移增长，
	// 所以判据是「不少于早期基线的 28 个」而不是钉死一个数 —— 钉死会让每次加权限点都要改测试。
	if n < 28 {
		t.Fatalf("库存域权限点应不少于 28 个（后续迁移只会增加），实际 %d", n)
	}

	for _, code := range []string{"inventory:stock_change", "inventory:stock_deduct", "inventory:movement_list",
		"inventory:reason_list", "inventory:reason_create", "inventory:reason_update"} {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE permission_code = ?", code).Scan(&hit).Error; err != nil {
			t.Fatalf("查询权限点 %s 失败: %v", code, err)
		}
		if hit != 1 {
			t.Fatalf("权限点 %s 应已 seed，实际 %d 条", code, hit)
		}
	}

	for _, code := range []string{"inventory:cache_sync", "inventory:cache_reconcile"} {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE permission_code = ?", code).Scan(&hit).Error; err != nil {
			t.Fatalf("查询权限点 %s 失败: %v", code, err)
		}
		if hit != 0 {
			t.Fatalf("权限点 %s 已随库存缓存下线（迁移 122 删除），不该再出现，实际 %d 条 —— 检查 104 的 seed 与幂等条件是否又把它加了回来", code, hit)
		}
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND title = '库存管理' AND deleted_at IS NULL").Scan(&n).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("迁移 101 应 seed 「库存管理」后台菜单，实际 %d", n)
	}
}

func newInventoryPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"inventory:warehouse_create": true, "inventory:warehouse_update": true,
			"inventory:warehouse_delete": true, "inventory:reason_create": true,

			"inventory:stock_change": true, "inventory:reason_update": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"inventory.warehouse_create": true, "inventory.warehouse_update": true,
			"inventory.warehouse_delete": true, "inventory.reason_create": true,

			"inventory.stock_change": true, "inventory.reason_update": true,
		})
	})
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	// 生产装配在基础层挂成功/失败出口与请求取词；缺了它们写响应会落成 JSON 或空响应。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", pageview.Slot(inventoryhttp.InventoryListPageTemplate, pageview.RenderPage), handle.InventoryPage)

	engine.GET("/admin/inventory/warehouses", pageview.Slot(inventoryhttp.InventoryWarehousesPageTemplate, pageview.RenderPage), handle.InventoryWarehousesPage)
	engine.GET("/admin/inventory/reasons", pageview.Slot(inventoryhttp.InventoryReasonsPageTemplate, pageview.RenderPage), handle.InventoryReasonsPage)
	engine.POST("/admin/inventory/warehouse/create", pageview.HTMLFragment, handle.InventoryWarehouseCreate)
	engine.POST("/admin/inventory/warehouse/update", pageview.HTMLFragment, handle.InventoryWarehouseUpdate)
	engine.POST("/admin/inventory/warehouse/default", pageview.HTMLFragment, handle.InventoryWarehouseDefault)
	engine.POST("/admin/inventory/warehouse/delete", pageview.HTMLFragment, handle.InventoryWarehouseDelete)
	engine.POST("/admin/inventory/stock/change", pageview.HTMLFragment, handle.InventoryStockChange)
	engine.POST("/admin/inventory/reason/create", pageview.HTMLFragment, handle.InventoryReasonCreate)
	engine.POST("/admin/inventory/reason/update", pageview.HTMLFragment, handle.InventoryReasonUpdate)
	return engine, f
}

func templateRoot() string {
	return "../../../../internal/templates"
}

func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func postForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func postJSON(t *testing.T, engine *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求体失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func assertVariantCreatedJump(t *testing.T, rec *httptest.ResponseRecorder, wantBack string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("写动作结论走整页提示，应为 200（不再是 302），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Fatalf("整页提示不应带 Location（否则浏览器会再跳一次），实际 %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("提示页必须 no-store（回跳目标随本次操作变化，缓存会让人看到上一条结论），实际 %q", got)
	}

	doc, err := html.Parse(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("提示页不是可解析的 HTML: %v", err)
	}

	card := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && hasAttr(n, "data-jump-state")
	})
	if card == nil {
		t.Fatalf("提示页缺少 data-jump-state 标记：%s", rec.Body.String())
	}
	if got := attrValue(card, "data-jump-state"); got != "ok" {
		t.Fatalf("变体创建成功应为 ok 态，实际 %q", got)
	}

	meta := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && strings.EqualFold(attrValue(n, "http-equiv"), "refresh")
	})
	if meta == nil {
		t.Fatal("成功提示页应有 meta refresh（1 秒后自动回跳）")
	}
	if got, want := attrValue(meta, "content"), "1;url="+wantBack; got != want {
		t.Fatalf("meta refresh 回跳目标应为 %q，实际 %q", want, got)
	}

	link := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attrValue(n, "class"), "btn-primary")
	})
	if link == nil {
		t.Fatal("成功提示页应有「立即前往」链接")
	}
	if got := attrValue(link, "href"); got != wantBack {
		t.Fatalf("「立即前往」应指向归属商品编辑页 %q，实际 %q", wantBack, got)
	}

	if msg := nodeText(findNode(doc, func(n *html.Node) bool { return attrValue(n, "class") == "jump-msg" })); strings.TrimSpace(msg) == "" {
		t.Fatal("提示页缺少结论文案")
	}
}

func productEditJumpTarget(projectID, productID string) string {
	q := url.Values{}
	q.Set("project", strings.TrimSpace(projectID))
	q.Set("product", strings.TrimSpace(productID))
	return "/admin/products/edit?" + q.Encode()
}

func findNode(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func attrValue(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			sb.WriteString(cur.Data)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return sb.String()
}

func countVariants(t *testing.T, f *invFixture, productID string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ?", productID).Scan(&n).Error; err != nil {
		t.Fatalf("统计变体失败: %v", err)
	}
	return n
}

func strPtr(s string) *string { return &s }
