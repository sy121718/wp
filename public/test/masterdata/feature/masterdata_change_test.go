// Package feature masterdata 模块 feature 测试 —— 主数据变更记录（issue #19）。
//
// 覆盖本票五条验收（真实 PostgreSQL + 生产迁移与 seed + 真实 service + 真实 Jet 渲染）：
//  1. 新增 / 修改 / 删除关键主数据时写入变更记录；
//  2. 至少覆盖变体默认发货仓、SKU 编码、商品价格、上下架状态、货源资料；
//  3. 记录含操作人与时间，append-only 不可改写；
//  4. 后台可按实体查询变更历史；
//  5. 与库存流水职责分离（配置变更与库存变动各记一处）。
//
// 装配与生产一致（routers.SetupRoutes）：masterdata 的契约注入 product 与 inventory
// 两个模块，商品的归属仓端口由 inventory 实现 —— 「建变体即留痕默认发货仓」走的是真实链路。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatahttp "go_wp/internal/module/masterdata/inbound/http"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
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

// operatorForTest 测试里的操作人（会话登录名在生产由 inbound 从 session 取）。
const operatorForTest = "alice"

// mdFixture 隔离 PG schema + 生产迁移与 seed + 真实工程行 + 真实三个模块 service。
type mdFixture struct {
	changes   *masterdataservice.Service
	products  *productservice.Service
	inventory *inventoryservice.Service
	projects  *projectservice.Service
	db        *gorm.DB
	projectID string
}

// newMDFixture 建隔离 schema 并装配三个模块（接线方式与 routers.SetupRoutes 一致）。
func newMDFixture(t *testing.T) *mdFixture {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "变更记录测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	changes := masterdataservice.NewService(masterdatamodel.NewModel(db), projects)
	inventory := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetVariantStock(inventory)
	inventory.SetVariantCost(products)
	products.SetMasterDataChanges(changes)
	inventory.SetMasterDataChanges(changes)
	return &mdFixture{
		changes: changes, products: products, inventory: inventory,
		projects: projects, db: db, projectID: project.ID,
	}
}

// —— 小工具 ——

func strPtr(s string) *string { return &s }

func floatPtr(v float64) *float64 { return &v }

// warehouse 建仓（工程内第一个仓自动成为默认仓）。
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

// product 建商品（首个变体自动生成）。
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

// variant 取商品的首个变体（商品恒有至少一个变体）。
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

// entityChanges 某实体的全部变更记录（时间倒序，分页放宽到 200 条一次取全）。
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

// findChange 在记录里找「某动作 + 某字段」那一条（找不到返回 nil）。
func findChange(rows []*masterdatadto.ChangeResp, action, field string) *masterdatadto.ChangeResp {
	for _, r := range rows {
		if r.Action == action && r.Field == field {
			return r
		}
	}
	return nil
}

// mustChange 断言存在某条记录且旧值 / 新值符合预期。
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

// describeChanges 变更记录的简要快照（失败信息用）。
func describeChanges(rows []*masterdatadto.ChangeResp) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, r.Action+"/"+r.Field+"("+r.OldValue+"→"+r.NewValue+")")
	}
	return strings.Join(parts, ", ")
}

// changeCount 某实体的记录条数。
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

// movementCount 某变体的库存流水条数（验收 5 的对照面）。
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

// —— 验收 1 + 2：新增关键主数据写入记录，且覆盖默认发货仓 / SKU 编码 ——

func TestMasterDataChangeCreateCoversWarehouseAndSKU(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "测试 T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	// 商品级新增：字段级落行（上下架状态在列，初始 draft）。
	productRows := f.entityChanges(t, masterdataenums.EntityProduct, p.ID)
	if len(productRows) == 0 {
		t.Fatalf("新增商品应写入变更记录")
	}
	for _, r := range productRows {
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

	// 变体级新增：SKU 编码 + 默认发货仓（id 与短码）+ 售价。
	variantRows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	skuRow := mustChange(t, variantRows, masterdataenums.ActionCreate, "sku_code", "", v.SKUCode)
	if !strings.HasPrefix(v.SKUCode, "SZ_") {
		t.Fatalf("SKU 编码应以归属仓短码开头，实际 %q", v.SKUCode)
	}
	// 展示文案由模块 enums 统一给出（页面直接渲染，不再维护第二份映射）。
	if skuRow.FieldLabel != "SKU 编码" {
		t.Fatalf("sku_code 的展示名应是「SKU 编码」，实际 %q", skuRow.FieldLabel)
	}
	mustChange(t, variantRows, masterdataenums.ActionCreate, "home_warehouse_id", "", sz.ID)
	mustChange(t, variantRows, masterdataenums.ActionCreate, "home_warehouse_code", "", sz.Code)
	mustChange(t, variantRows, masterdataenums.ActionCreate, "price", "", "0.00")

	// 未指定仓库时兜底默认仓：再建一个商品（不传 warehouseId）→ 记录里仍是 SZ。
	p2 := f.product(t, "第二件 T 恤", "tee-2", "")
	v2 := f.variant(t, p2.ID)
	rows2 := f.entityChanges(t, masterdataenums.EntityProductVariant, v2.ID)
	mustChange(t, rows2, masterdataenums.ActionCreate, "home_warehouse_code", "", "SZ")
}

// —— 验收 2：修改只记真正变化的字段（上下架状态 / 商品价格 / SKU 编码）——

func TestMasterDataChangeUpdateOnlyChangedFields(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	// ① 上架：状态一行，旧值 draft、新值 published。
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

	// ② 同值重复提交不产生记录（打开表单什么都没改就保存，审计里不留噪声）。
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, Status: strPtr(productenums.StatusPublished), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("重复上架失败: %v", err)
	}
	if n := f.changeCount(t, masterdataenums.EntityProduct, p.ID); n != int64(len(rows)) {
		t.Fatalf("同值更新不应新增记录，实际从 %d 变成 %d", len(rows), n)
	}

	// ③ 变体改价 + 改 SKU 编码：两条记录，各自的新旧值可读。
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

// —— 验收 1：删除同样留痕（new 为空、old 为删除前的取值）——

func TestMasterDataChangeDeleteRecordsOldValues(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	// ① 删变体。
	if err := f.products.DeleteVariant(ctx, &productdto.DeleteVariantReq{
		ID: v.ID, OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("删变体失败: %v", err)
	}
	vRows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	mustChange(t, vRows, masterdataenums.ActionDelete, "sku_code", v.SKUCode, "")
	mustChange(t, vRows, masterdataenums.ActionDelete, "price", "0.00", "")

	// ② 删商品：连带它的变体一起留痕（否则「商品的变体去哪了」查不出来）。
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

// —— 验收 2：货源资料（含类型 / 关联方 / 结算价 / 对接配置）——

func TestMasterDataChangeSourceLifecycle(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	src, err := f.inventory.CreateSource(ctx, &inventorydto.CreateSourceReq{
		ProjectID: f.projectID, Code: "SZ_FACTORY", Name: "苏州自有工厂",
		Type: inventoryenums.SourceTypeInternal, SettlePrice: floatPtr(3.5),
		Config: json.RawMessage(`{"erp":"sap"}`), OperatorID: operatorForTest,
	})
	if err != nil {
		t.Fatalf("建货源失败: %v", err)
	}
	rows := f.entityChanges(t, masterdataenums.EntityInventorySource, src.ID)
	mustChange(t, rows, masterdataenums.ActionCreate, "code", "", "SZ_FACTORY")
	mustChange(t, rows, masterdataenums.ActionCreate, "type", "", inventoryenums.SourceTypeInternal)
	// 内部货源恒为关联方（DDL CHECK 兜住），留痕记的是落库后的真值。
	mustChange(t, rows, masterdataenums.ActionCreate, "related_party", "", "true")
	mustChange(t, rows, masterdataenums.ActionCreate, "settle_price", "", "3.50")
	mustChange(t, rows, masterdataenums.ActionCreate, "config", "", `{"erp":"sap"}`)
	for _, r := range rows {
		if r.EntityTypeLabel != "货源" {
			t.Fatalf("实体类型展示名应是「货源」，实际 %q", r.EntityTypeLabel)
		}
	}

	// 改：停用 + 改结算价 + 换对接配置 —— 只有这三个字段产生记录。
	if _, err = f.inventory.UpdateSource(ctx, &inventorydto.UpdateSourceReq{
		ID: src.ID, Status: strPtr(inventoryenums.SourceStatusDisabled),
		SettlePrice: floatPtr(4.2), Config: json.RawMessage(`{"erp":"kingdee"}`),
		OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("改货源失败: %v", err)
	}
	rows = f.entityChanges(t, masterdataenums.EntityInventorySource, src.ID)
	mustChange(t, rows, masterdataenums.ActionUpdate, "status", inventoryenums.SourceStatusActive, inventoryenums.SourceStatusDisabled)
	mustChange(t, rows, masterdataenums.ActionUpdate, "settle_price", "3.50", "4.20")
	mustChange(t, rows, masterdataenums.ActionUpdate, "config", `{"erp":"sap"}`, `{"erp":"kingdee"}`)
	if findChange(rows, masterdataenums.ActionUpdate, "type") != nil {
		t.Fatalf("没改类型就不该有 type 记录：%s", describeChanges(rows))
	}
	if findChange(rows, masterdataenums.ActionUpdate, "code") != nil {
		t.Fatalf("没改编码就不该有 code 记录：%s", describeChanges(rows))
	}

	// 删：old 为删除前的取值。
	if err = f.inventory.DeleteSource(ctx, &inventorydto.DeleteSourceReq{ID: src.ID, OperatorID: operatorForTest}); err != nil {
		t.Fatalf("删货源失败: %v", err)
	}
	rows = f.entityChanges(t, masterdataenums.EntityInventorySource, src.ID)
	mustChange(t, rows, masterdataenums.ActionDelete, "name", "苏州自有工厂", "")
}

// —— 验收 3：操作人与时间 + append-only 不可改写 ——

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
	// 操作人逐条落库：创建时是 alice，改价那条是 bob。
	priceRow := findChange(rows, masterdataenums.ActionUpdate, "price")
	if priceRow == nil || priceRow.OperatorID != "bob" {
		t.Fatalf("改价记录的操作人应是 bob，实际 %+v", priceRow)
	}
	// 时间：全部非空，且「改价」不早于「创建」。
	createRow := findChange(rows, masterdataenums.ActionCreate, "price")
	if createRow == nil || createRow.CreatedAt == "" || priceRow.CreatedAt == "" {
		t.Fatalf("记录必须带时间：create=%+v update=%+v", createRow, priceRow)
	}
	if priceRow.CreatedAt < createRow.CreatedAt {
		t.Fatalf("后发生的变更时间不应早于先发生的：%s < %s", priceRow.CreatedAt, createRow.CreatedAt)
	}
	// 操作人过滤是查询维度之一。
	byBob, err := f.changes.ListChanges(ctx, &masterdatadto.ListChangeReq{
		ProjectID: f.projectID, EntityID: v.ID, OperatorID: "bob", Size: 200,
	})
	if err != nil || len(byBob) != 1 || byBob[0].Field != "price" {
		t.Fatalf("按操作人筛选应只命中改价那条，实际 %v %d", err, len(byBob))
	}

	// append-only：数据库层直接拒绝改写（代码层也没有更新 / 删除入口）。
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

// —— 验收 5：与库存流水职责分离 ——

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

	// 库存变动（入库 +5）：只进流水，不进主数据变更记录。
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn,
		ReasonCode: "purchase_in", SourceType: inventoryenums.MovementSourcePurchaseOrder,
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

	// 配置变更（改价）：只进主数据变更记录，不进库存流水。
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

// —— 验收 2：价格改动的三条来源路径（变体编辑 / 定价工具 / 入库成本回写）——

func TestMasterDataChangePriceOrigins(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	// 成本价（变体编辑路径）。
	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: v.ID, CostPrice: floatPtr(40), OperatorID: operatorForTest,
	}); err != nil {
		t.Fatalf("写成本价失败: %v", err)
	}
	rows := f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	if row := mustChange(t, rows, masterdataenums.ActionUpdate, "cost_price", "", "40.00"); row.Origin != masterdataenums.OriginVariant {
		t.Fatalf("变体编辑路径的 origin 应是 %s，实际 %s", masterdataenums.OriginVariant, row.Origin)
	}

	// 定价工具批量改价（来源 = pricing）：成本 40 × 1.5 = 60。
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

	// 入库成本价回写（来源 = receipt）：成本价 40 → 22.5。
	if err := f.products.UpdateVariantCost(ctx, v.ID, 22.5, operatorForTest); err != nil {
		t.Fatalf("成本价回写失败: %v", err)
	}
	rows = f.entityChanges(t, masterdataenums.EntityProductVariant, v.ID)
	if row := mustChange(t, rows, masterdataenums.ActionUpdate, "cost_price", "40.00", "22.50"); row.Origin != masterdataenums.OriginReceipt {
		t.Fatalf("入库回写的 origin 应是 %s，实际 %s", masterdataenums.OriginReceipt, row.Origin)
	}
	// 回写只动成本价一列：售价那条记录仍是定价工具写的。
	priceRow := findChange(rows, masterdataenums.ActionUpdate, "price")
	if priceRow == nil || priceRow.NewValue != "60.00" {
		t.Fatalf("成本价回写不该动售价，实际 %+v", priceRow)
	}
}

// —— 验收 4：接口链路（按实体 / 按条件查询）——

func TestMasterDataChangeAPIChain(t *testing.T) {
	f := newMDFixture(t)
	if f == nil {
		return
	}
	sz := f.warehouse(t, "SZ", "苏州仓")
	p := f.product(t, "T 恤", "tee", sz.ID)
	v := f.variant(t, p.ID)

	engine := newMasterDataAPIEngine(f)

	// 字段级列表。
	rec := httptestGet(engine, "/api/masterdata/change/list?projectId="+f.projectID+
		"&entityType="+masterdataenums.EntityProductVariant+"&entityId="+v.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"field":"sku_code"`) {
		t.Fatalf("变更列表接口应返回字段级记录，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 计数。
	rec = httptestGet(engine, "/api/masterdata/change/count?projectId="+f.projectID+"&entityId="+v.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total"`) {
		t.Fatalf("计数接口应返回 total，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 实体清单（按实体聚合）。
	rec = httptestGet(engine, "/api/masterdata/change/entities?projectId="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changeCount"`) {
		t.Fatalf("实体清单接口应返回聚合结果，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 单实体时间线。
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
	// 实体 id 不是 uuid：明确 400，而不是静默返回空集或 500。
	rec = httptestGet(engine, "/api/masterdata/change/list?projectId="+f.projectID+"&entityId=not-a-uuid")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), masterdataenums.ErrInvalidParam) {
		t.Fatalf("非法实体 id 应 400 + %s，实际 %d：%s", masterdataenums.ErrInvalidParam, rec.Code, rec.Body.String())
	}
	// 实体类型不在白名单：明确 400。
	rec = httptestGet(engine, "/api/masterdata/change/list?projectId="+f.projectID+"&entityType=order")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), masterdataenums.ErrEntityTypeInvalid) {
		t.Fatalf("白名单外实体类型应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// newMasterDataAPIEngine 只挂变更记录 JSON 接口的测试引擎（真实 handler + pkg/response）。
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

// httptestGet 发一个 GET 请求（页面与接口断言共用）。
func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// 编译期护栏：测试断言用的契约类型来自模块 contract（避免误用内部实现）。
var _ masterdatacontract.MasterDataService = (*masterdataservice.Service)(nil)
