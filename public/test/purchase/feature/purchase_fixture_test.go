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

	"go_wp/pkg/i18n"

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

var slugSeq atomic.Int64

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
	// 失败区文案只从词库取词：把本用例库里的词条灌进缓存，不读会漂移的全局库。
	var rows []struct {
		ItemKey string `gorm:"column:item_key"`
		Lang    string
		Value   string `gorm:"column:value"`
	}
	if err := db.Table("sys_i18n").Select("item_key, lang, item_value AS value").Where("status = ?", 1).Scan(&rows).Error; err != nil {
		t.Fatalf("读取词条失败: %v", err)
	}
	words := make(map[string]map[string]string, len(rows))
	for _, r := range rows {
		if words[r.ItemKey] == nil {
			words[r.ItemKey] = map[string]string{}
		}
		words[r.ItemKey][r.Lang] = r.Value
	}
	i18n.InjectForTest(words, nil)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "采购测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects, stubMasterDataChanges{})
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

func assertInventoryJump(t *testing.T, rec *httptest.ResponseRecorder, state string, wants ...string) {
	t.Helper()
	if state == "ok" {
		if rec.Code != http.StatusOK {
			t.Fatalf("成功应为 200 + HTML，实际 %d：%s", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Fatalf("成功不该给 Location（导航在前端），实际 %q", loc)
		}
		if nav := rec.Header().Get("HX-Redirect"); nav != "" {
			t.Fatalf("成功不该给 HX-Redirect（导航在前端），实际 %q", nav)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Fatalf("成功响应 Content-Type = %q，want text/html", ct)
		}
		return
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("失败应为 4xx/5xx + HTML，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("失败响应 Content-Type = %q，want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("响应缺少 %q：%s", want, body)
		}
	}
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

var _ purchasecontract.StockPort = (*purchasestock.Stock)(nil)

var (
	_ inventorycontract.ProductStockLookup = (*inventoryservice.Service)(nil)
	_ inventorycontract.BatchStockWriter   = (*inventoryservice.Service)(nil)
	_ productcontract.VariantCostPort      = (*productservice.Service)(nil)
	_ purchasecontract.PurchaseService     = (*purchaseservice.Service)(nil)
)

func bareSKU(code, warehouseCode string) string {
	return productservice.StripWarehousePrefix(code, warehouseCode)
}

func countMovements(t *testing.T, f *purchaseFixture, variantID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE variant_id = ?", variantID).Scan(&n).Error; err != nil {
		t.Fatalf("统计流水失败: %v", err)
	}
	return n
}

func trueSourceTotal(t *testing.T, f *purchaseFixture, variantID string) int {
	t.Helper()
	var total int
	if err := f.db.Raw("SELECT COALESCE(SUM(quantity),0) FROM inventory_stocks WHERE variant_id = ?",
		variantID).Scan(&total).Error; err != nil {
		t.Fatalf("读真源汇总失败: %v", err)
	}
	return total
}

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

func (f *purchaseFixture) stockQty(t *testing.T, variantID, warehouseID string) int {
	t.Helper()
	var qty int
	if err := f.db.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
		variantID, warehouseID).Scan(&qty).Error; err != nil {
		t.Fatalf("读库存真源失败: %v", err)
	}
	return qty
}

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
