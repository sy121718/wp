package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"
	purchaseservice "go_wp/internal/module/purchase/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const operatorForTest = "alice"

type mdFixture struct {
	changes   *masterdataservice.Service
	products  *productservice.Service
	inventory *inventoryservice.Service
	purchase  *purchaseservice.Service
	projects  *projectservice.Service
	db        *gorm.DB
	projectID string
}

func newMDFixture(t *testing.T) *mdFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "变更记录测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	changes := masterdataservice.NewService(masterdatamodel.NewModel(db), projects)
	inventory := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects, changes)
	products.SetInventoryService(inventory)
	inventory.SetVariantWarehouseStocks(products)
	purchase := purchaseservice.NewService(purchasemodel.NewModel(db), projects)
	purchase.SetVariantCost(products)
	purchase.SetMasterDataChanges(changes)
	return &mdFixture{
		changes: changes, products: products, inventory: inventory, purchase: purchase,
		projects: projects, db: db, projectID: project.ID,
	}
}

func strPtr(s string) *string { return &s }

func floatPtr(v float64) *float64 { return &v }

func (f *mdFixture) warehouse(t *testing.T, code, name string) *inventorydto.WarehouseResp {
	t.Helper()
	w, err := f.inventory.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: code, Name: name,
	})
	if err != nil {
		t.Fatalf("建仓 %s 失败: %v", code, err)
	}
	return w
}

func (f *mdFixture) product(t *testing.T, name, slug string, warehouseID string) *productdto.ProductResp {
	t.Helper()
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		WarehouseID: warehouseID, OperatorID: operatorForTest,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", slug, err)
	}
	return p
}

func (f *mdFixture) variant(t *testing.T, productID string) *productdto.VariantResp {
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

func (f *mdFixture) entityChanges(t *testing.T, entityType, entityID string) []*masterdatadto.ChangeResp {
	t.Helper()
	rows, err := f.changes.ListChanges(context.Background(), &masterdatadto.ListChangeReq{
		ProjectID: f.projectID, EntityType: entityType, EntityID: entityID, Size: 200,
	})
	if err != nil {
		t.Fatalf("查变更记录失败: %v", err)
	}
	return rows
}

func findChange(rows []*masterdatadto.ChangeResp, action, field string) *masterdatadto.ChangeResp {
	for _, r := range rows {
		if r.Action == action && r.Field == field {
			return r
		}
	}
	return nil
}

func mustChange(t *testing.T, rows []*masterdatadto.ChangeResp, action, field, oldValue, newValue string) *masterdatadto.ChangeResp {
	t.Helper()
	row := findChange(rows, action, field)
	if row == nil {
		t.Fatalf("缺少 %s/%s 的变更记录，实际有 %d 条：%s", action, field, len(rows), describeChanges(rows))
	}
	if row.OldValue != oldValue || row.NewValue != newValue {
		t.Fatalf("%s/%s 应是 %q → %q，实际 %q → %q", action, field, oldValue, newValue, row.OldValue, row.NewValue)
	}
	return row
}

func describeChanges(rows []*masterdatadto.ChangeResp) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, r.Action+"/"+r.Field+"("+r.OldValue+"→"+r.NewValue+")")
	}
	return strings.Join(parts, ", ")
}

func (f *mdFixture) changeCount(t *testing.T, entityType, entityID string) int64 {
	t.Helper()
	n, err := f.changes.CountChanges(context.Background(), &masterdatadto.ListChangeReq{
		ProjectID: f.projectID, EntityType: entityType, EntityID: entityID,
	})
	if err != nil {
		t.Fatalf("统计变更记录失败: %v", err)
	}
	return n
}

func (f *mdFixture) movementCount(t *testing.T, variantID string) int {
	t.Helper()
	list, err := f.inventory.ListMovements(context.Background(), &inventorydto.ListMovementReq{
		ProjectID: f.projectID, VariantID: variantID, Size: 200,
	})
	if err != nil {
		t.Fatalf("查库存流水失败: %v", err)
	}
	return len(list)
}

func TestMasterDataChangeCreateCoversWarehouseAndSKU(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "测试 T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	productRows := f.entityChanges(t, masterdataenums.EntityProduct, p.ID)
	if len(productRows) == 0 {
		t.Fatalf("新增商品应写入变更记录")
	}
	for _, r := range productRows {

		if len(r.ID) != 36 || r.ID[14] != '7' {
			t.Fatalf("变更记录主键应为 UUIDv7，实际 id=%q", r.ID)
		}
		if r.Action != masterdataenums.ActionCreate {
			t.Fatalf("新增商品的记录动作应是 create，实际 %s", r.Action)
		}
		if r.EntityLabel != "测试 T 恤" {
			t.Fatalf("实体展示名应是商品名快照，实际 %q", r.EntityLabel)
		}
		if r.OperatorID != operatorForTest {
			t.Fatalf("记录应含操作人 %q，实际 %q", operatorForTest, r.OperatorID)
		}
		if r.CreatedAt == "" {
			t.Fatalf("记录应含时间")
		}
	}
	mustChange(t, productRows, masterdataenums.ActionCreate, "status", "", productenums.StatusDraft)

	variantRows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	skuRow := mustChange(t, variantRows, masterdataenums.ActionCreate, "sku_code", "", v.SKUCode)
	if !strings.HasPrefix(v.SKUCode, "SZ_") {
		t.Fatalf("SKU 编码应以归属仓短码开头，实际 %q", v.SKUCode)
	}

	if skuRow.FieldLabel != "SKU 编码" {
		t.Fatalf("sku_code 的展示名应是「SKU 编码」，实际 %q", skuRow.FieldLabel)
	}
	mustChange(t, variantRows, masterdataenums.ActionCreate, "home_warehouse_id", "", sz.ID)
	mustChange(t, variantRows, masterdataenums.ActionCreate, "home_warehouse_code", "", sz.Code)
	mustChange(t, variantRows, masterdataenums.ActionCreate, "price", "", "0.00")

	p2 := f.product(t, "第二件 T 恤", "tee-2", "")
	v2 := f.variant(t, p2.ID)
	rows2 := f.entityChanges(t, masterdataenums.EntityProductVariant, v2.ID)
	mustChange(t, rows2, masterdataenums.ActionCreate, "home_warehouse_code", "", "SZ")
}

func TestMasterDataChangeUpdateOnlyChangedFields(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, Status: strPtr(productenums.StatusPublished), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("上架失败: %v", err)
	}
	rows := f.entityChanges(t, masterdataenums.EntityProduct, p.ID)
	mustChange(t, rows, masterdataenums.ActionUpdate, "status", productenums.StatusDraft, productenums.StatusPublished)
	updateRows := 0
	for _, r := range rows {
		if r.Action == masterdataenums.ActionUpdate {
			updateRows++
		}
	}
	if updateRows != 1 {
		t.Fatalf("只改了状态就该只有 1 条修改记录，实际 %d 条：%s", updateRows, describeChanges(rows))
	}

	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, Status: strPtr(productenums.StatusPublished), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("重复上架失败: %v", err)
	}
	if n := f.changeCount(t, masterdataenums.EntityProduct, p.ID); n != int64(len(rows)) {
		t.Fatalf("同值更新不应新增记录，实际从 %d 变成 %d", len(rows), n)
	}

	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: v.ID, Price: floatPtr(128.5), SKUCode: strPtr("SZ_TEE_900"), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("改变体失败: %v", err)
	}
	vRows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	mustChange(t, vRows, masterdataenums.ActionUpdate, "price", "0.00", "128.50")
	mustChange(t, vRows, masterdataenums.ActionUpdate, "sku_code", v.SKUCode, "SZ_TEE_900")
	if findChange(vRows, masterdataenums.ActionUpdate, "home_warehouse_id") != nil {
		t.Fatalf("编辑路径没有默认发货仓可写，不该出现该字段的记录：%s", describeChanges(vRows))
	}
}

func TestMasterDataChangeDeleteRecordsOldValues(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	if err := f.products.DeleteVariant(ctx, &productdto.DeleteVariantReq{
		ID: v.ID, OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("删变体失败: %v", err)
	}
	vRows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	mustChange(t, vRows, masterdataenums.ActionDelete, "sku_code", v.SKUCode, "")
	mustChange(t, vRows, masterdataenums.ActionDelete, "price", "0.00", "")

	p2 := f.product(t, "第二件", "tee-2", sz.ID)
	v2 := f.variant(t, p2.ID)
	if err := f.products.Delete(ctx, &productdto.DeleteReq{ID: p2.ID, OperatorID: operatorForTest}); err != nil {
		t.Fatalf("删商品失败: %v", err)
	}
	mustChange(t, f.entityChanges(t, masterdataenums.EntityProduct, p2.ID),
		masterdataenums.ActionDelete, "name", "第二件", "")
	mustChange(t, f.entityChanges(t, masterdataenums.EntityProductVariant, v2.ID),
		masterdataenums.ActionDelete, "sku_code", v2.SKUCode, "")
}

func TestMasterDataChangeSourceLifecycle(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	src, err := f.purchase.CreateSource(ctx, &purchasedto.CreateSourceReq{
		ProjectID: f.projectID, Code: "SZ_FACTORY", Name: "苏州自有工厂",
		Type: purchaseenums.SourceTypeInternal, SettlePrice: floatPtr(3.5),
		Config: json.RawMessage(`{"erp":"sap"}`), OperatorID: operatorForTest,
	})
	if err != nil {
		t.Fatalf("建货源失败: %v", err)
	}
	rows := f.entityChanges(t, masterdataenums.EntityInventorySource, src.ID)
	mustChange(t, rows, masterdataenums.ActionCreate, "code", "", "SZ_FACTORY")
	mustChange(t, rows, masterdataenums.ActionCreate, "type", "", purchaseenums.SourceTypeInternal)

	mustChange(t, rows, masterdataenums.ActionCreate, "related_party", "", "true")
	mustChange(t, rows, masterdataenums.ActionCreate, "settle_price", "", "3.50")
	mustChange(t, rows, masterdataenums.ActionCreate, "config", "", `{"erp":"sap"}`)
	for _, r := range rows {
		if r.EntityTypeLabel != "货源" {
			t.Fatalf("实体类型展示名应是「货源」，实际 %q", r.EntityTypeLabel)
		}
	}

	if _, err = f.purchase.UpdateSource(ctx, &purchasedto.UpdateSourceReq{
		ID: src.ID, Status: strPtr(purchaseenums.SourceStatusDisabled),
		SettlePrice: floatPtr(4.2), Config: json.RawMessage(`{"erp":"kingdee"}`),
		OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("改货源失败: %v", err)
	}
	rows = f.entityChanges(t, masterdataenums.EntityInventorySource, src.ID)
	mustChange(t, rows, masterdataenums.ActionUpdate, "status", purchaseenums.SourceStatusActive, purchaseenums.SourceStatusDisabled)
	mustChange(t, rows, masterdataenums.ActionUpdate, "settle_price", "3.50", "4.20")
	mustChange(t, rows, masterdataenums.ActionUpdate, "config", `{"erp":"sap"}`, `{"erp":"kingdee"}`)
	if findChange(rows, masterdataenums.ActionUpdate, "type") != nil {
		t.Fatalf("没改类型就不该有 type 记录：%s", describeChanges(rows))
	}
	if findChange(rows, masterdataenums.ActionUpdate, "code") != nil {
		t.Fatalf("没改编码就不该有 code 记录：%s", describeChanges(rows))
	}

	if err = f.purchase.DeleteSource(ctx, &purchasedto.DeleteSourceReq{ID: src.ID, OperatorID: operatorForTest}); err != nil {
		t.Fatalf("删货源失败: %v", err)
	}
	rows = f.entityChanges(t, masterdataenums.EntityInventorySource, src.ID)
	mustChange(t, rows, masterdataenums.ActionDelete, "name", "苏州自有工厂", "")
}

func TestMasterDataChangeAppendOnlyWithOperatorAndTime(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)
	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: v.ID, Price: floatPtr(66), OperatorID: "bob",
	}); err != nil {
		t.Fatalf("改变体失败: %v", err)
	}

	rows, err := f.changes.ListChanges(ctx, &masterdatadto.ListChangeReq{
		ProjectID: f.projectID, EntityType: masterdataenums.EntityProductVariant, EntityID: v.ID, Size: 200,
	})
	if err != nil || len(rows) == 0 {
		t.Fatalf("应有变更记录，实际 %v %d", err, len(rows))
	}

	priceRow := findChange(rows, masterdataenums.ActionUpdate, "price")
	if priceRow == nil || priceRow.OperatorID != "bob" {
		t.Fatalf("改价记录的操作人应是 bob，实际 %+v", priceRow)
	}

	createRow := findChange(rows, masterdataenums.ActionCreate, "price")
	if createRow == nil || createRow.CreatedAt == "" || priceRow.CreatedAt == "" {
		t.Fatalf("记录必须带时间：create=%+v update=%+v", createRow, priceRow)
	}
	if priceRow.CreatedAt < createRow.CreatedAt {
		t.Fatalf("后发生的变更时间不应早于先发生的：%s < %s", priceRow.CreatedAt, createRow.CreatedAt)
	}

	byBob, err := f.changes.ListChanges(ctx, &masterdatadto.ListChangeReq{
		ProjectID: f.projectID, EntityID: v.ID, OperatorID: "bob", Size: 200,
	})
	if err != nil || len(byBob) != 1 || byBob[0].Field != "price" {
		t.Fatalf("按操作人筛选应只命中改价那条，实际 %v %d", err, len(byBob))
	}

	if err := f.db.Exec("UPDATE master_data_changes SET new_value = '999.00' WHERE id = ?", priceRow.ID).Error; err == nil {
		t.Fatalf("UPDATE 变更记录应被数据库拒绝")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("拒绝原因应是 append-only，实际 %v", err)
	}
	if err := f.db.Exec("DELETE FROM master_data_changes WHERE id = ?", priceRow.ID).Error; err == nil {
		t.Fatalf("DELETE 变更记录应被数据库拒绝")
	}
	var still string
	if err := f.db.Raw("SELECT new_value FROM master_data_changes WHERE id = ?", priceRow.ID).Scan(&still).Error; err != nil {
		t.Fatalf("记录不应被删除: %v", err)
	}
	if still != "66.00" {
		t.Fatalf("记录值不应被改写，实际 %q", still)
	}
}

func TestMasterDataChangeSeparatedFromStockMovements(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	before := f.changeCount(t, masterdataenums.EntityProductVariant, v.ID)
	if f.movementCount(t, v.ID) != 0 {
		t.Fatalf("新建变体不该有库存流水")
	}

	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn,
		ReasonCode: "purchase_in", SourceType: purchaseenums.MovementSourcePurchaseOrder,
		SourceRef: "PO-TEST-1", OperatorID: operatorForTest,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: sz.ID, Quantity: 5,
		}},
	}); err != nil {
		t.Fatalf("库存变动失败: %v", err)
	}
	if n := f.movementCount(t, v.ID); n != 1 {
		t.Fatalf("入库应写 1 条库存流水，实际 %d", n)
	}
	if n := f.changeCount(t, masterdataenums.EntityProductVariant, v.ID); n != before {
		t.Fatalf("库存数量变动不该写主数据变更记录（实际从 %d 变成 %d）", before, n)
	}

	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: v.ID, Price: floatPtr(88), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("改变体失败: %v", err)
	}
	if n := f.changeCount(t, masterdataenums.EntityProductVariant, v.ID); n != before+1 {
		t.Fatalf("改价应写 1 条变更记录（从 %d 到 %d）", before, n+0)
	}
	if n := f.movementCount(t, v.ID); n != 1 {
		t.Fatalf("改价不该写库存流水，实际 %d 条", n)
	}
}

func TestMasterDataChangePriceOrigins(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: v.ID, CostPrice: floatPtr(40), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("写成本价失败: %v", err)
	}
	rows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	if row := mustChange(t, rows, masterdataenums.ActionUpdate, "cost_price", "", "40.00"); row.Origin != masterdataenums.OriginVariant {
		t.Fatalf("变体编辑路径的 origin 应是 %s，实际 %s", masterdataenums.OriginVariant, row.Origin)
	}

	applied, err := f.products.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: productdto.PricingRuleReq{
			ProjectID: f.projectID, RuleType: productenums.PricingRuleCostMultiple,
			RuleParams: json.RawMessage(`{"multiplier":1.5}`),
			Scope:      productenums.PricingScopeSKU, TargetID: v.ID,
		},
		Note: "成本上涨", OperatorID: operatorForTest,
	})
	if err != nil {
		t.Fatalf("应用定价失败: %v", err)
	}
	if applied.ChangedCount != 1 {
		t.Fatalf("应改动 1 个变体，实际 %d", applied.ChangedCount)
	}
	rows = f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	if row := mustChange(t, rows, masterdataenums.ActionUpdate, "price", "0.00", "60.00"); row.Origin != masterdataenums.OriginPricing {
		t.Fatalf("定价路径的 origin 应是 %s，实际 %s", masterdataenums.OriginPricing, row.Origin)
	}

	if err := f.products.UpdateVariantCost(ctx, f.projectID, v.ID, 22.5, operatorForTest); err != nil {
		t.Fatalf("成本价回写失败: %v", err)
	}
	rows = f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	if row := mustChange(t, rows, masterdataenums.ActionUpdate, "cost_price", "40.00", "22.50"); row.Origin != masterdataenums.OriginReceipt {
		t.Fatalf("入库回写的 origin 应是 %s，实际 %s", masterdataenums.OriginReceipt, row.Origin)
	}

	priceRow := findChange(rows, masterdataenums.ActionUpdate, "price")
	if priceRow == nil || priceRow.NewValue != "60.00" {
		t.Fatalf("成本价回写不该动售价，实际 %+v", priceRow)
	}
}

func TestMasterDataChangeAPIChain(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	engine := newMasterDataAPIEngine(f)

	rec := httptestGet(engine, "/api/masterdata/change/list?projectId="+f.projectID+
		"&entityType="+masterdataenums.EntityProductVariant+"&entityId="+v.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"field":"sku_code"`) {
		t.Fatalf("变更列表接口应返回字段级记录，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = httptestGet(engine, "/api/masterdata/change/count?projectId="+f.projectID+"&entityId="+v.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total"`) {
		t.Fatalf("计数接口应返回 total，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = httptestGet(engine, "/api/masterdata/change/entities?projectId="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changeCount"`) {
		t.Fatalf("实体清单接口应返回聚合结果，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = httptestGet(engine, "/api/masterdata/change/entity?projectId="+f.projectID+
		"&entityType="+masterdataenums.EntityProductVariant+"&entityId="+v.ID)
	var timeline struct {
		Code int                              `json:"code"`
		Data masterdatadto.EntityTimelineResp `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("解析时间线响应失败: %v", err)
	}
	if rec.Code != http.StatusOK || timeline.Data.Total == 0 || timeline.Data.EntityLabel != v.SKUCode {
		t.Fatalf("单实体时间线不符：%d %+v", rec.Code, timeline.Data)
	}

	rec = httptestGet(engine, "/api/masterdata/change/list?projectId="+f.projectID+"&entityId=not-a-uuid")
	if rec.Code != http.StatusBadRequest || !mentionsMasterDataError(rec.Body.String(), masterdataenums.ErrInvalidParam, "参数不合法") {
		t.Fatalf("非法实体 id 应 400 + %s，实际 %d：%s", masterdataenums.ErrInvalidParam, rec.Code, rec.Body.String())
	}

	rec = httptestGet(engine, "/api/masterdata/change/list?projectId="+f.projectID+"&entityType=order")
	if rec.Code != http.StatusBadRequest || !mentionsMasterDataError(rec.Body.String(), masterdataenums.ErrEntityTypeInvalid, "实体类型不在变更记录的白名单内") {
		t.Fatalf("白名单外实体类型应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

func mentionsMasterDataError(body, errKey, zhText string) bool {
	return strings.Contains(body, errKey) || strings.Contains(body, zhText)
}

func newMasterDataAPIEngine(f *mdFixture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := masterdatahttp.NewHandle(f.changes)
	engine.GET("/api/masterdata/change/list", handle.ListChanges)
	engine.GET("/api/masterdata/change/count", handle.CountChanges)
	engine.GET("/api/masterdata/change/entities", handle.ListEntities)
	engine.GET("/api/masterdata/change/entity", handle.EntityTimeline)
	return engine
}

func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

var _ masterdatacontract.MasterDataService = (*masterdataservice.Service)(nil)
