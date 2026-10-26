// Package feature purchase 模块 feature 测试（货源 / 采购单 / 入库）。
//
// 采购域 2026-10 从 inventory 拆出：本包夹具不再借用库存的 fixture，
// 自带 purchase service + 库存契约端口（经 purchasestock 适配器，与生产装配同形）。
package feature

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventorymodel "go_wp/internal/module/inventory/model"
	purchasestock "go_wp/internal/module/inventory/outbound/purchasestock"
	inventoryservice "go_wp/internal/module/inventory/service"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasemodel "go_wp/internal/module/purchase/model"
	purchaseservice "go_wp/internal/module/purchase/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// slugSeq 让同一次测试里建的商品 slug 不撞车（商品 slug 工程内唯一）。
var slugSeq atomic.Int64

// purchaseFixture 隔离 PG schema + 生产迁移与 seed + 真实工程行 + 真实的
// inventory / product / purchase 三个 service。
//
// 接线与生产装配（internal/routers/assembly.go）逐条对齐：
// inventory 实现商品侧的两个端口（ProductStockPort / VariantAvailabilityPort），
// purchase 经 purchasestock 适配器拿库存能力、经 VariantCostPort 写商品成本价。
type purchaseFixture struct {
	purchase  *purchaseservice.Service
	stock     purchasecontract.StockPort
	catalog   purchasecontract.VariantCatalog
	inventory *inventoryservice.Service
	products  *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	projectID string
}

func newPurchaseFixture(t *testing.T) *purchaseFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "采购测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)
	purchase := purchaseservice.NewService(purchasemodel.NewModel(db), projects)
	lookup, ok := any(inv).(inventorycontract.ProductStockLookup)
	if !ok {
		t.Fatalf("库存 service 未实现 ProductStockLookup（契约漂移）")
	}
	batch, ok := any(inv).(inventorycontract.BatchStockWriter)
	if !ok {
		t.Fatalf("库存 service 未实现 BatchStockWriter（契约漂移）")
	}
	stock := purchasestock.New(inv, lookup, batch)
	purchase.SetStock(stock)
	purchase.SetVariantCost(products)
	return &purchaseFixture{
		purchase: purchase, stock: stock, inventory: inv, products: products,
		db: db, projects: projects, projectID: project.ID,
		catalog: purchasestock.NewCatalog(products, inv),
	}
}

// assertInventoryJump 断言一次写动作的响应形态（与库存侧同名助手同口径）。
//
//   - state = "ok"：200 + JSON，且不带 Location / HX-Redirect（导航由前端 data-write-nav 决定）；
//   - state = "err"：4xx/5xx + JSON，wants 逐条匹配响应体（即对外文案）。
func assertInventoryJump(t *testing.T, rec *httptest.ResponseRecorder, state string, wants ...string) {
	t.Helper()
	if state == "ok" {
		if rec.Code != http.StatusOK {
			t.Fatalf("成功应为 200 + JSON，实际 %d：%s", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Fatalf("成功不该给 Location（导航在前端），实际 %q", loc)
		}
		if nav := rec.Header().Get("HX-Redirect"); nav != "" {
			t.Fatalf("成功不该给 HX-Redirect（导航在前端），实际 %q", nav)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("成功响应 Content-Type = %q，want application/json", ct)
		}
		return
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("失败应为 4xx/5xx + JSON，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败响应 Content-Type = %q，want application/json", ct)
	}
	body := rec.Body.String()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("响应缺少 %q：%s", want, body)
		}
	}
}

// templateRoot 模板根（本包测试只做真实的 Jet 渲染）。
func templateRoot() string {
	return "../../../../internal/templates"
}

// httptestGet 发一个 GET 请求（页面渲染断言用）。
func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// postForm 发送 application/x-www-form-urlencoded 表单。
func postForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// 编译期断言：适配器必须满足采购侧的库存端口（与生产同一处断言）。
var _ purchasecontract.StockPort = (*purchasestock.Stock)(nil)

// 编译期断言：库存 service 必须同时实现采购要用的两条端口。
var (
	_ inventorycontract.ProductStockLookup = (*inventoryservice.Service)(nil)
	_ inventorycontract.BatchStockWriter   = (*inventoryservice.Service)(nil)
	_ productcontract.VariantCostPort      = (*productservice.Service)(nil)
	_ purchasecontract.PurchaseService     = (*purchaseservice.Service)(nil)
)

// bareSKU 仓库侧裸码：库存行与流水的 sku_code 存的是**剥掉仓码前缀**后的编码
// （商品 / 变体侧仍带前缀，如 SZ_TEE1 → 仓库侧 TEE1；规则见 docs/14 §1.1 与
// productservice.StripWarehousePrefix）。库存侧的读写与断言都用它换算 ——
// 拿商品侧编码去查库存表 / 断言流水，钉的就是已被废弃的旧口径。
func bareSKU(code, warehouseCode string) string {
	return productservice.StripWarehousePrefix(code, warehouseCode)
}

// countMovements 统计某变体的库存流水条数（断言「入库确实写了流水」）。
func countMovements(t *testing.T, f *purchaseFixture, variantID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE variant_id = ?", variantID).Scan(&n).Error; err != nil {
		t.Fatalf("统计流水失败: %v", err)
	}
	return n
}

// trueSourceTotal 读某变体的**真源**汇总（跨仓求和）—— 替代已删的展示缓存列。
func trueSourceTotal(t *testing.T, f *purchaseFixture, variantID string) int {
	t.Helper()
	var total int
	if err := f.db.Raw("SELECT COALESCE(SUM(quantity),0) FROM inventory_stocks WHERE variant_id = ?",
		variantID).Scan(&total).Error; err != nil {
		t.Fatalf("读真源汇总失败: %v", err)
	}
	return total
}

// createWarehouse 建仓小工具（采购收货必须有仓；首个仓自动成为默认仓）。
func (f *purchaseFixture) createWarehouse(t *testing.T, code, name string, isDefault bool) *inventorydto.WarehouseResp {
	t.Helper()
	w, err := f.inventory.CreateWarehouse(context.Background(), &inventorydto.CreateWarehouseReq{
		ProjectID: f.projectID, Code: code, Name: name, IsDefault: isDefault,
	})
	if err != nil {
		t.Fatalf("建仓 %s 失败: %v", code, err)
	}
	return w
}

// stockQty 直读真源表的数量（断言落库结果，不经 service）。
func (f *purchaseFixture) stockQty(t *testing.T, variantID, warehouseID string) int {
	t.Helper()
	var qty int
	if err := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&qty).Error; err != nil {
		t.Fatalf("读库存真源失败: %v", err)
	}
	return qty
}

// mustProduct 建商品并返回它（首个变体已由商品模块自动创建）。
func mustProduct(t *testing.T, f *purchaseFixture, name string) *productdto.ProductResp {
	t.Helper()
	slug := fmt.Sprintf("%s-%d", strings.ToLower(name), slugSeq.Add(1))
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return p
}

// firstVariant 取商品的首个变体（商品恒有至少一个变体）。
func (f *purchaseFixture) firstVariant(t *testing.T, productID string) *productdto.VariantResp {
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
