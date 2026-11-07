package feature

import (
	"context"
	"errors"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	"go_wp/public/test/support"
)

type wpickFixture struct {
	inventory *inventoryservice.Service
	products  *productservice.Service
	projects  *projectservice.Service

	// rewireProducts 用另一份审计端口重建商品服务（审计是构造依赖，故障注入只能换装配）。
	rewireProducts func(changes masterdatacontract.MasterDataService)
	db             *gorm.DB
	projectID      string

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
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)
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
	fx := &wpickFixture{
		inventory: inv, products: products, projects: projects,
		db: db, projectID: project.ID, sz: sz, sh: sh,
	}
	fx.rewireProducts = func(changes masterdatacontract.MasterDataService) {
		fx.products = productservice.NewService(productmodel.NewModel(db), projects, changes)
		fx.products.SetInventoryService(inv)
	}
	return fx
}

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

func (f *wpickFixture) wpickSeedWarehouseSKU(t *testing.T, warehouseID, skuCode, productID, variantID string) {
	t.Helper()
	if _, err := f.inventory.EnsureStock(context.Background(), &inventorydto.EnsureStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID,
		ProductID: productID, VariantID: variantID, SKUCode: skuCode,
	}); err != nil {
		t.Fatalf("登记仓库 SKU %s 失败: %v", skuCode, err)
	}
}

func (f *wpickFixture) wpickContainerSKU(t *testing.T, productID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM products WHERE id = ?", productID).Scan(&code).Error; err != nil {
		t.Fatalf("读 products.sku_code 失败: %v", err)
	}
	return code
}

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

func (f *wpickFixture) wpickStockCode(t *testing.T, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读 inventory_stocks.sku_code 失败: %v", err)
	}
	return code
}

func (f *wpickFixture) wpickProductCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM products WHERE project_id = ?", f.projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计商品数失败: %v", err)
	}
	return n
}

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

	if _, rows := f.wpickExternalSKU(t, picked.Variants[0].ID, f.sh.ID); rows != 0 {
		t.Fatalf("认领已有编码不应为该变体新建库存行，实际 %d 行", rows)
	}

	if code := f.wpickStockCode(t, src.Variants[0].ID, f.sh.ID); code != "TEE-001" {
		t.Fatalf("仓库侧的编码应是裸码 TEE-001（不带仓码前缀），实际 %q", code)
	}

	if external, rows := f.wpickExternalSKU(t, src.Variants[0].ID, f.sh.ID); rows != 1 || external != "" {
		t.Fatalf("认领不改写既有行：应仍是 1 行且外码空，实际 %d 行 / %q", rows, external)
	}

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

func TestProductCreateFromWarehouseSKUNotFound(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}

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

	_, err = f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "没选货", Slug: "picked-no-sku",
		SKUSource: "warehouse", WarehouseID: f.sh.ID,
	})
	if err == nil || err.Error() != inventoryenums.ErrWarehouseSKURequired {
		t.Fatalf("没给仓库 SKU 应返回 %s，实际 %v", inventoryenums.ErrWarehouseSKURequired, err)
	}
}

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

func TestProductCreateFromWarehouseSKUExternalConflict(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	owner := f.wpickProduct(t, "外码占用者", "pick-owner")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "OWNER-1", owner.ID, owner.Variants[0].ID)

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

	assertNoTraceOfSKU(t, f, "SH_CONFLICT-1", "CONFLICT-1")
}

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

func TestProductCreateInvalidExternalSKURejectsBeforeWrite(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "非法外码", Slug: "invalid-external-port",
		SKUCode: "INVALID-EXT", ExternalSKU: "line\nbreak",
	})
	if err == nil || err.Error() != inventoryenums.ErrExternalSKUInvalid {
		t.Fatalf("非法外码应由库存端口拒绝为 %s，实际 %v", inventoryenums.ErrExternalSKUInvalid, err)
	}
	var count int64
	if err := f.db.Table("products").Where("project_id = ? AND slug = ?", f.projectID, "invalid-external-port").Count(&count).Error; err != nil {
		t.Fatalf("核查商品行失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("外码失败后不应留商品行，实际 %d", count)
	}
}

func TestProductCreateExternalSKUUsesInjectedPort(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	port := &rejectExternalSKU{ProductStockPort: f.inventory}
	f.products.SetInventoryService(port)
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "外码端口探针", Slug: "external-port-probe",
		SKUCode: "PORT-PROBE", ExternalSKU: "EXT-PROBE",
	})
	if !errors.Is(err, errExternalSKUProbe) || port.calls != 1 {
		t.Fatalf("商品创建应调用注入的归一端口一次，调用=%d 错误=%v", port.calls, err)
	}
}

var errExternalSKUProbe = errors.New("external SKU port probe")

type rejectExternalSKU struct {
	inventorycontract.ProductStockPort
	calls int
}

func (p *rejectExternalSKU) NormalizeExternalSKU(string) (string, error) {
	p.calls++
	return "", errExternalSKUProbe
}

func TestProductCreateMissingStockPortRejectsExternalSKU(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	f.products.SetInventoryService(nil)
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "无库存端口", Slug: "missing-stock-port",
		SKUCode: "NO-PORT", ExternalSKU: "EXT-NO-PORT",
	})
	if err == nil || err.Error() != inventoryenums.ErrStockWarehouseNeeded {
		t.Fatalf("未注入端口时显式外码必须失败，实际 %v", err)
	}
}

func TestNewPageRendersWarehouseSKUCatalog(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	host := f.wpickProduct(t, "抽屉候选宿主", "picker-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "PICK-777", host.ID, host.Variants[0].ID)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
		for _, entry := range c.Errors {
			if _, ok := templates.AsTemplateRenderError(entry.Err); ok {
				t.Errorf("商品新建页模板执行失败：%v", entry.Err)
			}
		}
	})
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", pageview.UseRenderer(pageview.RenderPage), handle.ProductsPage)
	engine.GET("/admin/products/new", pageview.UseRenderer(pageview.RenderPage), handle.ProductNewPage)

	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("商品新建页应 200，实际 %d", rec.Code)
	}
	page := rec.Body.String()
	for _, want := range []string{
		`name="externalSku"`, `data-sku-candidates`,
		`value="PICK-777"`, `data-warehouse="` + f.sh.ID + `"`, `data-sku-input`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建表单缺少 %q（仓库候选目录没渲染出来）", want)
		}
	}

	for _, unwanted := range []string{`name="skuSource"`, `name="warehouseSku"`} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("新建表单不该再有 %q（这两条来路已从页面撤掉）", unwanted)
		}
	}
}

func TestProductCreateAttachesExistingStock(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	loose, err := f.inventory.CreateStock(ctx, &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: f.sh.ID, SKUCode: "LOOSE-777",
		Quantity: 12, TrackQuantity: true,
	})
	if err != nil {
		t.Fatalf("建货行失败: %v", err)
	}

	price := 29.9
	picked, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "从仓库选已有的货", Slug: "picked-loose",
		DefaultPrice: &price, SKUSource: "warehouse",
		WarehouseSKU: "LOOSE-777", WarehouseID: f.sh.ID,
	})
	if err != nil {
		t.Fatalf("从仓库选建商品失败: %v", err)
	}
	variantID := picked.Variants[0].ID

	var raw []byte
	if qerr := f.db.Raw("SELECT warehouse_skus FROM product_variants WHERE id = ?", variantID).
		Row().Scan(&raw); qerr != nil {
		t.Fatalf("读变体引用失败: %v", qerr)
	}
	refs := productmodel.DecodeWarehouseSKUs(raw)
	found := false
	for _, r := range refs {
		if r.StockID == loose.ID && r.WarehouseID == f.sh.ID && r.SKUCode == "LOOSE-777" {
			found = true
		}
	}
	if !found {
		t.Fatalf("变体应引用货行 %q，实际 %s", loose.ID, string(raw))
	}

	var qty int
	if qerr := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE id = ?", loose.ID).
		Row().Scan(&qty); qerr != nil {
		t.Fatalf("读货行失败: %v", qerr)
	}
	if qty != 12 {
		t.Fatalf("引用不该改货行数量（期望 12），实际 %d", qty)
	}

	var n int64
	if qerr := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		f.sh.ID, "LOOSE-777").Scan(&n).Error; qerr != nil {
		t.Fatalf("查同仓同码行数失败: %v", qerr)
	}
	if n != 1 {
		t.Fatalf("引用之后同仓同码应仍只有 1 行，实际 %d 行", n)
	}

	port, ok := interface{}(f.products).(productcontract.VariantWarehouseStockPort)
	if !ok {
		t.Fatalf("商品服务未实现 VariantWarehouseStockPort")
	}
	owners, oerr := port.VariantIDsByWarehouseStock(ctx, f.projectID, loose.ID)
	if oerr != nil {
		t.Fatalf("按货行反查变体失败: %v", oerr)
	}
	if len(owners) != 1 || owners[0] != variantID {
		t.Fatalf("按货行 %q 应反查到变体 %q，实际 %v", loose.ID, variantID, owners)
	}

}
