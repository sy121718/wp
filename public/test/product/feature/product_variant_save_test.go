package feature

import (
	"bytes"
	"context"
	"errors"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

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

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "变体清单测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)
	products.SetInventory(inventorymodel.NewModel(db))
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

func (f *variantListFixture) variantsOf(t *testing.T, productID string) []*productdto.VariantResp {
	t.Helper()
	detail, err := f.products.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail.Variants
}

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

func (f *variantListFixture) save(t *testing.T, productID string, rows []productdto.VariantListResp) *productdto.SaveVariantListResp {
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

func (f *variantListFixture) saveErr(t *testing.T, productID string, rows []productdto.VariantListResp) string {
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

	after := f.variantsOf(t, p.ID)
	if len(after) != 1 {
		t.Fatalf("预览不该写库：期望仍是 1 个变体，实际 %d", len(after))
	}
	if after[0].SKUCode != before[0].SKUCode {
		t.Fatalf("预览不该改库里的 SKU：%s → %s", before[0].SKUCode, after[0].SKUCode)
	}
}

func TestVariantPreviewSkipsExistingAndReappears(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "外套", "coat-preview", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	if res := f.save(t, p.ID, rows); res.Created != 4 {
		t.Fatalf("首次保存应新增 4 个组合，实际 %d", res.Created)
	}
	stored := f.variantsOf(t, p.ID)
	if len(stored) != 4 {
		t.Fatalf("保存后应有 4 个变体（无规格占位变体已随清单外删除），实际 %d", len(stored))
	}

	existing := make([]json.RawMessage, 0, len(stored))
	for _, v := range stored {
		existing = append(existing, v.OptionValues)
	}
	again := f.previewAll(t, p.ID, existing...)
	if len(again.Rows) != 0 || again.Skipped != 4 {
		t.Fatalf("重复生成不该追加：rows=%d skipped=%d", len(again.Rows), again.Skipped)
	}

	if res := f.previewAll(t, p.ID); len(res.Rows) != 0 || res.Skipped != 4 {
		t.Fatalf("库里已有的组合应跳过：rows=%d skipped=%d", len(res.Rows), res.Skipped)
	}

	dropped := stored[0]
	keep := make([]productdto.VariantListResp, 0, len(stored)-1)
	for _, v := range stored[1:] {
		keep = append(keep, productdto.VariantListResp{VariantID: v.ID, SKUCode: v.SKUCode, OptionValues: v.OptionValues})
	}
	if res := f.save(t, p.ID, keep); res.Deleted != 1 {
		t.Fatalf("清单外的那一行应被删除，实际 deleted=%d", res.Deleted)
	}

	back := f.previewAll(t, p.ID)
	if len(back.Rows) != 1 {
		t.Fatalf("删掉的那一个组合应能被重新生成，实际 rows=%d", len(back.Rows))
	}
	if optionKeyOf(t, back.Rows[0].OptionValues) != optionKeyOf(t, dropped.OptionValues) {
		t.Fatalf("重新生成的组合应与被删的那个一致")
	}
}

func TestVariantSaveAddsUpdatesAndDeletes(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "卫衣", "hoodie-save", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)

	first := []productdto.VariantListResp{
		{OptionValues: pv.Rows[0].OptionValues},
		{OptionValues: pv.Rows[1].OptionValues},
	}
	if res := f.save(t, p.ID, first); res.Created != 2 || res.Deleted != 1 {
		t.Fatalf("首次保存应新增 2 个、删掉清单外的占位变体：created=%d deleted=%d", res.Created, res.Deleted)
	}

	v1 := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	custom := "SZ_CUSTOM_RED_S_V"
	second := []productdto.VariantListResp{
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

	for _, v := range after {
		if optionKeyOf(t, v.OptionValues) == optionKeyOf(t, pv.Rows[1].OptionValues) {
			t.Fatalf("清单外的那一行应被删除")
		}
	}
}

func TestVariantSaveRejectsStockRow(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "夹克", "jacket-skip", []string{color.ID, size.ID})

	pv := f.previewAll(t, p.ID)
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	f.save(t, p.ID, rows)

	withStock := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	kept := variantByOption(t, f, p.ID, pv.Rows[3].OptionValues)
	f.addStock(t, withStock, 3)

	before := f.variantsOf(t, p.ID)
	_, err := f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{ProjectID: f.projectID, ProductID: p.ID, WarehouseID: f.warehouse.ID, Rows: []productdto.VariantListResp{{VariantID: kept.ID, SKUCode: kept.SKUCode + "_EDIT", OptionValues: kept.OptionValues}}})
	var rejected facing.BlockedError
	if !errors.As(err, &rejected) || len(rejected.BlockedItems()) != 1 || rejected.BlockedItems()[0].ID != withStock.ID || rejected.BlockedItems()[0].ReasonKey != productenums.VariantSkipHasStock {
		t.Fatalf("应整批拒绝并定位库存行：%v", err)
	}
	after := f.variantsOf(t, p.ID)
	if len(after) != len(before) {
		t.Fatalf("拒绝后应零删除：before=%d after=%d", len(before), len(after))
	}
	codes := map[string]string{}
	for _, v := range before {
		codes[v.ID] = v.SKUCode
	}
	for _, v := range after {
		if codes[v.ID] != v.SKUCode {
			t.Fatalf("拒绝后不应提交 SKU 更新：%s", v.ID)
		}
	}

}

func TestVariantSaveRejectsIllegalCombinationAndEmptySKU(t *testing.T) {
	f := newVariantListFixture(t)
	if f == nil {
		return
	}
	color := f.mkAttr(t, "颜色", "color", []string{"red", "blue"})
	size := f.mkAttr(t, "尺寸", "size", []string{"s", "m"})
	p := f.mkProduct(t, "长裤", "pants-guard", []string{color.ID, size.ID})

	msg := f.saveErr(t, p.ID, []productdto.VariantListResp{{
		OptionValues: json.RawMessage(`{"nope":"x"}`),
	}})
	if !strings.Contains(msg, productenums.ErrVariationAttributeInvalid) {
		t.Fatalf("非法属性组应报 %s，实际 %q", productenums.ErrVariationAttributeInvalid, msg)
	}

	msg = f.saveErr(t, p.ID, []productdto.VariantListResp{{
		OptionValues: json.RawMessage(`{"color":"purple","size":"s"}`),
	}})
	if !strings.Contains(msg, productenums.ErrVariationValueInvalid) {
		t.Fatalf("非法取值应报 %s，实际 %q", productenums.ErrVariationValueInvalid, msg)
	}

	msg = f.saveErr(t, p.ID, []productdto.VariantListResp{{OptionValues: json.RawMessage(`{}`)}})
	if !strings.Contains(msg, productenums.ErrVariantOptionsInvalid) {
		t.Fatalf("空组合应报 %s，实际 %q", productenums.ErrVariantOptionsInvalid, msg)
	}

	pv := f.previewAll(t, p.ID)
	saved := f.save(t, p.ID, []productdto.VariantListResp{{OptionValues: pv.Rows[0].OptionValues}})
	if saved.Created != 1 {
		t.Fatalf("新增行应落库 1 个变体，实际 %d", saved.Created)
	}

	stored := variantByOption(t, f, p.ID, pv.Rows[0].OptionValues)
	msg = f.saveErr(t, p.ID, []productdto.VariantListResp{{
		VariantID: stored.ID, SKUCode: "   ", OptionValues: stored.OptionValues,
	}})
	if !strings.Contains(msg, productenums.ErrVariantSKUEmpty) {
		t.Fatalf("空 SKU 应报 %s，实际 %q", productenums.ErrVariantSKUEmpty, msg)
	}
	msg = f.saveErr(t, p.ID, []productdto.VariantListResp{{
		VariantID: stored.ID, SKUCode: "SZ__RED_V", OptionValues: stored.OptionValues,
	}})
	if !strings.Contains(msg, productenums.ErrVariantSKUEmpty) {
		t.Fatalf("规范化后为空段应报 %s，实际 %q", productenums.ErrVariantSKUEmpty, msg)
	}

	_, conflictErr := f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{ProjectID: f.projectID, ProductID: p.ID, WarehouseID: f.warehouse.ID, Rows: []productdto.VariantListResp{{SKUCode: stored.SKUCode, OptionValues: pv.Rows[1].OptionValues}}})
	var conflict facing.BlockedError
	if !errors.As(conflictErr, &conflict) || len(conflict.BlockedItems()) != 1 || conflict.BlockedItems()[0].ID != "row:1" || conflict.BlockedItems()[0].ReasonKey != productenums.ErrSkuTaken {
		t.Fatalf("抄用已占用的 SKU 应定位到提交行：%v", conflictErr)
	}

	_, err := f.products.SaveVariantList(context.Background(), &productdto.SaveVariantListReq{ProjectID: f.projectID, ProductID: p.ID, WarehouseID: f.warehouse.ID, Rows: []productdto.VariantListResp{{VariantID: stored.ID, SKUCode: stored.SKUCode + "_EDIT"}, {VariantID: stored.ID, SKUCode: stored.SKUCode}}})
	var rejected facing.BlockedError
	if !errors.As(err, &rejected) || len(rejected.BlockedItems()) != 1 || rejected.BlockedItems()[0].ReasonKey != productenums.ErrVariantListDuplicated {
		t.Fatalf("重复行必须拒绝：%v", err)
	}
	for _, v := range f.variantsOf(t, p.ID) {
		if v.ID == stored.ID && v.SKUCode != stored.SKUCode {
			t.Fatalf("重复拒绝后发生了写入：%+v", v)
		}
	}

}

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
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	engine.POST("/admin/products/variant/preview", handle.PreviewVariant)

	engine.POST("/api/product/variant/save", producthttp.NewProductHandle(f.products).SaveVariantList)

	rec := postForm(engine, "/admin/products/variant/preview", url.Values{
		"projectId": {f.projectID}, "productId": {p.ID}, "warehouseId": {f.warehouse.ID},
		"attr:" + color.ID: {color.Values[0].ID},
		"attr:" + size.ID:  {size.Values[0].ID, size.Values[1].ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Code int
		Data productdto.PreviewVariantResp
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("预览响应不是合法 JSON: %v（%s）", err, rec.Body.String())
	}
	if preview.Code != 200 || len(preview.Data.Rows) != 2 {
		t.Fatalf("勾选 1×2 应返回 2 行，实际 codeOK=%v rows=%d", preview.Code == 200, len(preview.Data.Rows))
	}
	for _, row := range preview.Data.Rows {
		if row.SKUCode == "" || len(row.OptionValues) == 0 {
			t.Fatalf("预览行必须带 SKU / 原始规格组合：%+v", row)
		}
	}

	if got := f.variantsOf(t, p.ID); len(got) != 1 {
		t.Fatalf("预览不该写库：期望 1 个变体，实际 %d", len(got))
	}

	rec = postJSON(t, engine, "/api/product/variant/save", map[string]any{
		"projectId": f.projectID, "productId": p.ID, "warehouseId": f.warehouse.ID,
		"rows": []map[string]any{
			{"variantId": "", "skuCode": preview.Data.Rows[0].SKUCode, "optionValues": json.RawMessage(preview.Data.Rows[0].OptionValues)},
			{"variantId": "", "skuCode": preview.Data.Rows[1].SKUCode, "optionValues": json.RawMessage(preview.Data.Rows[1].OptionValues)},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var saved struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Created int `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("保存响应不是合法 JSON: %v（%s）", err, rec.Body.String())
	}

	if saved.Code != http.StatusOK || saved.Message == "" || saved.Data.Created != 2 {
		t.Fatalf("回执应含新增计数与计数本身，实际 code=%d message=%q created=%d", saved.Code, saved.Message, saved.Data.Created)
	}
	if got := f.variantsOf(t, p.ID); len(got) != 2 {
		t.Fatalf("保存后应有 2 个变体，实际 %d", len(got))
	}
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

var _ = httptest.NewRecorder

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

	f.save(t, p.ID, []productdto.VariantListResp{
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
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	engine.GET("/admin/products/edit", pageview.UseRenderer(pageview.RenderPage), handle.ProductEditPage)
	rec := httptest.NewRecorder()

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

	for _, row := range pv.Rows[2:] {
		if strings.Contains(body, row.SKUCode) {
			t.Fatalf("未保存的组合出现在清单里（说明清单重算了笛卡尔积）：%s", row.SKUCode)
		}
	}
	if !strings.Contains(body, "保存变体清单") {
		t.Fatalf("清单区块应带保存入口")
	}
}
