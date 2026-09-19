// Package feature product 模块 feature 测试 —— 新建商品「从仓库选 SKU」（docs/14 §1.1 / §9.3）。
//
// 覆盖验收：成功（主体 SKU = <仓短码>_<仓库 SKU>、创建即入库并带 external_sku）、
// 仓库不存在、仓内没有这条仓库 SKU、主体 SKU 重复、bundle 拒绝该入口、
// 跨商品共用一个外部编码被拒（N:1 弱校验）、非法来源取值被拒。
//
// 服务端不信任前端：前端提交的 sku / 仓库编码都只是线索，落库前一律在该仓复核
// （GetWarehouseSKU），编码本体取自仓库那一行。断言直查真源列（products.sku_code /
// inventory_stocks.external_sku），不走 service 自己返回的响应。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"

	"go_wp/public/test/support"
)

// wpickFixture 隔离 PG schema + 生产迁移 + 真实工程 + 真实 inventory / product service。
type wpickFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	projects  *projectservice.Service
	db        *gorm.DB
	projectID string
	// sz 是默认仓；sh 是第二个仓 ——「从仓库选」都发生在第二个仓，
	// 免得与商品创建自动生成的库存行混在一起。
	sz *inventorydto.WarehouseResp
	sh *inventorydto.WarehouseResp
}

func newWPickFixture(t *testing.T) *wpickFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "从仓库选 SKU 测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
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
	return &wpickFixture{
		inventory: inv, products: products, projects: projects,
		db: db, projectID: project.ID, sz: sz, sh: sh,
	}
}

// wpickProduct 建一个普通商品并返回它（首个变体落在默认仓）。
func (f *wpickFixture) wpickProduct(t *testing.T, name, slug string) *productdto.ProductResp {
	t.Helper()
	price := 9.9
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return p
}

// wpickSeedWarehouseSKU 在指定仓给某个变体登记一条「仓库 SKU」（inventory_stocks 的一行）。
//
// 「仓库 SKU」不另建目录（docs/14 §4）：它就是库存真源上已有的 (warehouse_id, sku_code)。
func (f *wpickFixture) wpickSeedWarehouseSKU(t *testing.T, warehouseID, skuCode, productID, variantID string) {
	t.Helper()
	if _, err := f.inventory.EnsureStock(context.Background(), &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID,
		ProductID: productID, VariantID: variantID, SKUCode: skuCode,
	}); err != nil {
		t.Fatalf("登记仓库 SKU %s 失败: %v", skuCode, err)
	}
}

// wpickContainerSKU 直读 products.sku_code（主体 SKU 的唯一落点，迁移 246）。
func (f *wpickFixture) wpickContainerSKU(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

// wpickExternalSKU 直读真源里的外部编码与该行的条数（0 行表示「创建时没入库」）。
func (f *wpickFixture) wpickExternalSKU(t *testing.T, variantID, warehouseID string) (string, int) {
	t.Helper()
	var rows int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&rows).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if rows == 0 {
		return "", 0
	}
	var external string
	if err := f.db.Raw("SELECT external_sku FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&external).Error; err != nil {
		t.Fatalf("读 external_sku 失败: %v", err)
	}
	return external, rows
}

// wpickStockCode 直读真源里该 (仓库, 变体) 那一行的 sku_code（仓库侧编码 = 裸码）。
func (f *wpickFixture) wpickStockCode(t *testing.T, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读 inventory_stocks.sku_code 失败: %v", err)
	}
	return code
}

// wpickProductCount 当前工程里的商品数（判「被拒的商品没有落库」）。
func (f *wpickFixture) wpickProductCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM products WHERE project_id = ?", f.projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计商品数失败: %v", err)
	}
	return n
}

// TestProductCreateFromWarehouseSKU 成功路径：
// 主体 SKU = <仓短码>_<仓库 SKU>，首个变体就是它，**创建即入库**且 external_sku 带入。
func TestProductCreateFromWarehouseSKU(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	src := f.wpickProduct(t, "仓库里的货宿主", "pick-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "TEE-001", src.ID, src.Variants[0].ID)

	price := 19.9
	picked, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "从仓库选的衣服", Slug: "picked-tee", DefaultPrice: &price,
		SKUSource: "warehouse", WarehouseSKU: "TEE-001", WarehouseID: f.sh.ID,
		// 故意给一个**对不上**的 sku：服务端这条路径不采信它，编码本体取自仓库那一行。
		SKUCode: "FRONTEND-LIE",
	})
	if err != nil {
		t.Fatalf("从仓库选建商品失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, picked.ID); got != "SH_TEE-001" {
		t.Fatalf("主体 SKU 应为 SH_TEE-001（仓短码_仓库 SKU），实际 %q", got)
	}
	if len(picked.Variants) != 1 || picked.Variants[0].SKUCode != "SH_TEE-001" {
		t.Fatalf("首个变体应等于容器主体，实际 %+v", picked.Variants)
	}
	// 认领 / 复用（2026-09-19 口径）：仓库里那条 TEE-001 的行本来就在（属于宿主商品），
	// 商品侧**不新建** —— 以前这里会按带前缀的编码再建一行，正是「同仓同码两行」的来源。
	if _, rows := f.wpickExternalSKU(t, picked.Variants[0].ID, f.sh.ID); rows != 0 {
		t.Fatalf("认领已有编码不应为该变体新建库存行，实际 %d 行", rows)
	}
	// 宿主那一行原样保留：编码是**裸码**（不带仓码前缀），外码没被改写。
	if code := f.wpickStockCode(t, src.Variants[0].ID, f.sh.ID); code != "TEE-001" {
		t.Fatalf("仓库侧的编码应是裸码 TEE-001（不带仓码前缀），实际 %q", code)
	}
	if external, rows := f.wpickExternalSKU(t, src.Variants[0].ID, f.sh.ID); rows != 1 || external != "" {
		t.Fatalf("认领不改写既有行：应仍是 1 行且外码 TEE-001，实际 %d 行 / %q", rows, external)
	}

	// 自己打新码 = 建行：仓库侧写**裸码**，显式填的外部编码写在认领仓那一次。
	explicit, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "自己打码并带外部编码的商品", Slug: "picked-explicit",
		DefaultPrice: &price, SKUCode: "SELF-002",
		WarehouseID: f.sh.ID, ExternalSKU: "EXT-SELF-002",
	})
	if err != nil {
		t.Fatalf("带显式外部编码的创建失败: %v", err)
	}
	if got, _ := f.wpickExternalSKU(t, explicit.Variants[0].ID, f.sh.ID); got != "EXT-SELF-002" {
		t.Fatalf("显式填的外部编码应原样写入，实际 %q", got)
	}
	if code := f.wpickStockCode(t, explicit.Variants[0].ID, f.sh.ID); code != "SELF-002" {
		t.Fatalf("新码这条货在仓库侧应写成裸码 SELF-002，实际 %q", code)
	}
}

// TestProductCreateFromWarehouseSKUWarehouseMissing 仓库不存在：明确报业务错误，商品不落库。
func TestProductCreateFromWarehouseSKUWarehouseMissing(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	before := f.wpickProductCount(t)
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "不存在的仓", Slug: "picked-bad-warehouse",
		SKUSource: "warehouse", WarehouseSKU: "TEE-001",
		WarehouseID: "00000000-0000-0000-0000-000000000000",
	})
	if err == nil || !strings.HasPrefix(err.Error(), inventoryenums.ErrWarehouseNotFound) {
		t.Fatalf("仓库不存在应返回 %s，实际 %v", inventoryenums.ErrWarehouseNotFound, err)
	}
	if got := f.wpickProductCount(t); got != before {
		t.Fatalf("被拒的商品不该落库：before=%d after=%d", before, got)
	}
}

// TestProductCreateFromWarehouseSKUNotFound 该仓没有这条仓库 SKU（含「编码在别的仓」）。
func TestProductCreateFromWarehouseSKUNotFound(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	// 编码只出现在默认仓 SZ，选 SH 来找它 → 找不到（同一个编码可以在多个仓各有一条）。
	host := f.wpickProduct(t, "默认仓的货", "pick-host-sz")
	f.wpickSeedWarehouseSKU(t, f.sz.ID, "ONLY-IN-SZ", host.ID, host.Variants[0].ID)
	before := f.wpickProductCount(t)

	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "选错了仓", Slug: "picked-wrong-warehouse",
		SKUSource: "warehouse", WarehouseSKU: "ONLY-IN-SZ", WarehouseID: f.sh.ID,
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseSKUNotFound {
		t.Fatalf("该仓没有这条货应返回 %s，实际 %v", inventoryenums.ErrWarehouseSKUNotFound, err)
	}
	if got := f.wpickProductCount(t); got != before {
		t.Fatalf("被拒的商品不该落库：before=%d after=%d", before, got)
	}
	// 没给仓库 SKU 编码：同样是可行动的业务错误（不是内部错误）。
	_, err = f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "没选货", Slug: "picked-no-sku",
		SKUSource: "warehouse", WarehouseID: f.sh.ID,
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseSKURequired {
		t.Fatalf("没给仓库 SKU 应返回 %s，实际 %v", inventoryenums.ErrWarehouseSKURequired, err)
	}
}

// TestProductCreateFromWarehouseSKUDuplicate 主体 SKU 重复：同一条仓库 SKU 选两次被拒。
func TestProductCreateFromWarehouseSKUDuplicate(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	host := f.wpickProduct(t, "重复宿主", "pick-host-dup")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "DUP-001", host.ID, host.Variants[0].ID)

	req := func(name, slug string) *productdto.CreateReq {
		return &productdto.CreateReq{
			ProjectID: f.projectID, Name: name, Slug: slug,
			SKUSource: "warehouse", WarehouseSKU: "DUP-001", WarehouseID: f.sh.ID,
		}
	}
	if _, err := f.products.Create(ctx, req("第一次", "dup-first")); err != nil {
		t.Fatalf("第一次从仓库选应成功: %v", err)
	}
	before := f.wpickProductCount(t)
	_, err := f.products.Create(ctx, req("第二次", "dup-second"))
	if err == nil {
		t.Fatal("同一条仓库 SKU 再选一次应被拒（主体 SKU 在仓内 / 工程内重复）")
	}
	if got := f.wpickProductCount(t); got != before {
		t.Fatalf("被拒的商品不该落库：before=%d after=%d", before, got)
	}
}

// TestProductCreateFromWarehouseSKUBundleRejected 捆绑商品不存在于仓库：这个入口对它不成立。
func TestProductCreateFromWarehouseSKUBundleRejected(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	price := 99.0
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "捆绑也想从仓库选", Slug: "bundle-pick",
		Type: productmodel.TypeBundle, DefaultPrice: &price, SKUCode: "BUNDLE-PICK",
		SKUSource: "warehouse", WarehouseSKU: "TEE-001", WarehouseID: f.sh.ID,
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseSKUBundleNotAllowed {
		t.Fatalf("bundle 用「从仓库选」应返回 %s，实际 %v", inventoryenums.ErrWarehouseSKUBundleNotAllowed, err)
	}
}

// TestProductCreateFromWarehouseSKUExternalConflict 跨商品共用一个外部编码被拒（N:1 弱校验）。
//
// 触发路径是**自己打新码 + 显式填外码**：那条货还没进这个仓，商品侧的首次入库要写外码，
// 而外码已被另一个商品在本仓占用 —— 这是 N:1 弱校验要拦的形态。
// （「认领已有裸码」那条路不写外码：那一行属于别的商品，商品侧一个字都不改。）
//
// 顺带钉住**事务回滚**：商品 + 首个变体已经写进事务，随后库存行那一步抛业务错误 ——
// 整批回滚，products / product_variants / inventory_stocks 三张表都不该留痕。
func TestProductCreateFromWarehouseSKUExternalConflict(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	owner := f.wpickProduct(t, "外码占用者", "pick-owner")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "OWNER-1", owner.ID, owner.Variants[0].ID)
	// 占用者本人先把 EXT-SHARED 登记成**它自己**那一行的外部编码（同一商品，合法）。
	if _, err := f.inventory.BindExternalSKU(ctx, &inventorydto.BindExternalSKUReq{
		ProjectID: f.projectID, WarehouseID: f.sh.ID, VariantID: owner.Variants[0].ID,
		ExternalSKU: "EXT-SHARED",
	}); err != nil {
		t.Fatalf("占用者登记外部编码失败: %v", err)
	}

	before := f.wpickProductCount(t)
	_, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "想蹭外码的商品", Slug: "pick-conflict",
		SKUCode: "CONFLICT-1", WarehouseID: f.sh.ID, ExternalSKU: "EXT-SHARED",
	})
	if err == nil || !strings.HasPrefix(err.Error(), inventoryenums.ErrExternalSKUProductConflict) {
		t.Fatalf("跨商品共用外码应返回 %s，实际 %v", inventoryenums.ErrExternalSKUProductConflict, err)
	}
	if got := f.wpickProductCount(t); got != before {
		t.Fatalf("被拒的商品不该落库：before=%d after=%d", before, got)
	}
	// 三张表都不该有这个商品的任何痕迹（事务整体回滚）。
	assertNoTraceOfSKU(t, f, "SH_CONFLICT-1", "CONFLICT-1")
}

// TestProductCreateSKUSourceInvalid 来源取值非法：明确拒绝（不静默退化）。
func TestProductCreateSKUSourceInvalid(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "来源乱填", Slug: "bad-source", SKUSource: "warehouse2",
	})
	if err == nil || err.Error() != inventoryenums.ErrSKUSourceInvalid {
		t.Fatalf("非法来源应返回 %s，实际 %v", inventoryenums.ErrSKUSourceInvalid, err)
	}
}

// TestProductCreateCustomSourceUnchanged 自己创建这条来路一字未变：
// 编码原样（选了仓加前缀）、不做外码映射（除非显式填）、依然创建即入库。
func TestProductCreateCustomSourceUnchanged(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	price := 19.9
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "自己创建", Slug: "custom-self",
		DefaultPrice: &price, SKUCode: "SELF-9001",
	})
	if err != nil {
		t.Fatalf("自己创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SZ_SELF-9001" {
		t.Fatalf("选了默认仓应自动加仓码前缀，实际 %q", got)
	}
	external, rows := f.wpickExternalSKU(t, p.Variants[0].ID, f.sz.ID)
	if rows != 1 {
		t.Fatalf("创建即入库应有且只有一行，实际 %d", rows)
	}
	if external != "" {
		t.Fatalf("没填外部编码时应留空（= 该仓用我们自己的 SKU），实际 %q", external)
	}
}

// TestProductsPageRendersWarehouseSKUPicker 新建抽屉的「从仓库选」入口（页面侧，验收 3）：
// 服务端把该仓的候选按仓分组渲染进去，**字段名与 ProductsCreate 读取的完全一致** ——
// 抽屉里的字段名一旦分叉，运营选的货就会静默丢失（提交上去没人读，服务端按自己创建处理）。
func TestProductsPageRendersWarehouseSKUPicker(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	host := f.wpickProduct(t, "抽屉候选宿主", "picker-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "PICK-777", host.ID, host.Variants[0].ID)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", handle.ProductsPage)
	engine.GET("/admin/products/new", handle.ProductNewPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("商品列表页应 200，实际 %d", rec.Code)
	}
	page := rec.Body.String()
	for _, want := range []string{
		`name="skuSource"`, `name="warehouseSku"`, `name="externalSku"`,
		`value="PICK-777"`, `data-warehouse="` + f.sh.ID + `"`, `data-warehouse-sku`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建抽屉缺少 %q（「从仓库选」入口没渲染出来）", want)
		}
	}
}
