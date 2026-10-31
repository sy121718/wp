package feature

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	"go_wp/internal/templates"
)

func newStockSKUsPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"inventory:stock_ensure": true, "inventory:stock_change": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"inventory.stock_create": true, "inventory.stock_update": true,
		})
	})
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory/skus", adminui.PageSlot(inventoryhttp.InventoryStockSKUsPageTemplate), handle.InventoryStockSKUsPage)
	engine.POST("/admin/inventory/stock/create", handle.InventoryStockCreate)
	engine.POST("/admin/inventory/stock/update", handle.InventoryStockUpdate)
	engine.POST("/admin/inventory/stock/status", handle.InventoryStockStatus)
	return engine, f
}

func TestStockSKUsPageListsAllRows(t *testing.T) {
	engine, f := newStockSKUsPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	ctx := context.Background()

	if _, err := f.inventory.CreateStock(ctx, &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "RED-500", Name: "红茶 500ml",
		Quantity: 12, TrackQuantity: true, CostPrice: floatPtr(18.5),
	}); err != nil {
		t.Fatalf("建货行失败: %v", err)
	}

	if _, err := f.inventory.CreateStock(ctx, &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "GREEN-500", Name: "绿茶 500ml",
		Quantity: 3, TrackQuantity: true, CostPrice: floatPtr(2.4),
		CostCurrency: "USD", CostBaseCurrency: "CNY", Status: "disabled",
	}); err != nil {
		t.Fatalf("建停用货行失败: %v", err)
	}

	rec := httptestGet(engine, "/admin/inventory/skus")
	if rec.Code != 200 {
		t.Fatalf("台账页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"红茶 500ml", "RED-500", "绿茶 500ml", "GREEN-500",
		"苏州仓", "SZ",

		"已停用",

		"暂无商品引用",

		"汇率未固化",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("台账页缺少 %q", want)
		}
	}

	if !strings.Contains(page, "18.5") {
		t.Fatalf("成本列应显示 18.5（最短表示），页面里没有")
	}
}

func TestStockSKUWriteActions(t *testing.T) {
	engine, f := newStockSKUsPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	rec := postForm(engine, "/admin/inventory/stock/create", url.Values{
		"projectId": {f.projectID}, "warehouseId": {sz.ID}, "skuCode": {"TEA-001"},
		"name": {"乌龙 500ml"}, "barcode": {"6901234567890"},
		"quantity": {"7"}, "trackQuantity": {"1"}, "costPrice": {"21.00"}, "status": {"active"},
	})
	if rec.Code != 200 {
		t.Fatalf("新建货行应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var row struct {
		ID       string
		Name     string
		Barcode  string
		Quantity int
		Cost     *float64 `gorm:"column:cost_price"`
	}
	if err := f.db.Raw("SELECT id, name, barcode, quantity, cost_price FROM inventory_stocks "+
		"WHERE project_id = ? AND warehouse_id = ? AND sku_code = ?",
		f.projectID, sz.ID, "TEA-001").Scan(&row).Error; err != nil {
		t.Fatalf("读新建的行失败: %v", err)
	}
	if row.ID == "" || row.Name != "乌龙 500ml" || row.Barcode != "6901234567890" || row.Quantity != 7 {
		t.Fatalf("新建落库不对：%+v", row)
	}
	if row.Cost == nil || *row.Cost != 21 {
		t.Fatalf("新建时提交的成本价 21.00 应落库，实际 %v", row.Cost)
	}

	rec = postForm(engine, "/admin/inventory/stock/update", url.Values{
		"projectId": {f.projectID}, "id": {row.ID}, "name": {"乌龙 500ml（新包装）"},
		"costPrice": {"23.40"}, "status": {"active"},
	})
	if rec.Code != 200 {
		t.Fatalf("改主档应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var after struct {
		Name     string
		Quantity int
		Cost     *float64 `gorm:"column:cost_price"`
	}
	if err := f.db.Raw("SELECT name, quantity, cost_price FROM inventory_stocks WHERE id = ?", row.ID).
		Scan(&after).Error; err != nil {
		t.Fatalf("回读行失败: %v", err)
	}
	if after.Name != "乌龙 500ml（新包装）" || after.Quantity != 7 {
		t.Fatalf("改主档后应改名且数量仍为 7，实际 %+v", after)
	}
	if after.Cost == nil || *after.Cost != 23.40 {
		t.Fatalf("改主档后成本应为 23.40，实际 %v", after.Cost)
	}

	rec = postForm(engine, "/admin/inventory/stock/status", url.Values{
		"projectId": {f.projectID}, "id": {row.ID}, "status": {"disabled"},
	})
	if rec.Code != 200 {
		t.Fatalf("启停应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var status string
	if err := f.db.Raw("SELECT status FROM inventory_stocks WHERE id = ?", row.ID).Scan(&status).Error; err != nil {
		t.Fatalf("回读状态失败: %v", err)
	}
	if status != "disabled" {
		t.Fatalf("启停后状态应为 disabled，实际 %q", status)
	}

	if _, err := f.inventory.CreateStock(context.Background(), &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: sh.ID, SKUCode: "TEA-002", Name: "上海那批",
		Quantity: 1, TrackQuantity: true,
	}); err != nil {
		t.Fatalf("建第二个仓的行失败: %v", err)
	}
	rec = httptestGet(engine, "/admin/inventory/skus?project="+f.projectID+"&warehouseId="+sz.ID)
	if rec.Code != 200 {
		t.Fatalf("带仓筛选应 200，实际 %d", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "TEA-001") {
		t.Fatalf("按 SZ 筛后应只剩 SZ 的行，页面里没有 TEA-001")
	}
	if strings.Contains(page, "TEA-002") {
		t.Fatalf("按 SZ 筛后不该出现 SH 的行（TEA-002）")
	}

	if !strings.Contains(page, "TEA-001") {
		t.Fatalf("台账页不该隐藏停用行")
	}
}

func TestStockSKUsPageEmptyState(t *testing.T) {
	engine, f := newStockSKUsPageEngine(t)
	if engine == nil {
		return
	}
	rec := httptestGet(engine, "/admin/inventory/skus?project="+f.projectID)
	if rec.Code != 200 {
		t.Fatalf("空态页应 200，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "这个仓里还没有货") {
		t.Fatalf("空态页应有引导标题，实际：%s", rec.Body.String())
	}
}

var _ = httptest.NewRecorder
