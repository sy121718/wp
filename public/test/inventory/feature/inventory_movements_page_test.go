package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	"go_wp/internal/templates"
)

func newMovementsPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory/movements",
		pageview.Slot(inventoryhttp.InventoryMovementsPageTemplate, adminui.RenderPage), handle.InventoryMovementsPage)
	return engine, f
}

func seedStockRow(t *testing.T, f *invFixture, warehouseID, sku, name string, cost float64) string {
	t.Helper()
	row, err := f.inventory.CreateStock(context.Background(), &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID, SKUCode: sku, Name: name,
		CostPrice: floatPtr(cost), TrackQuantity: true,
	})
	if err != nil {
		t.Fatalf("建货行 %s 失败: %v", sku, err)
	}
	return row.ID
}

func changeStockOnRow(t *testing.T, f *invFixture, p interface{ GetID() string }, variantID, warehouseID, sku,
	direction, reason string, quantity int) {
	t.Helper()
	_, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: direction, ReasonCode: reason,
		SourceType: inventoryenums.MovementSourceInventoryPage, SourceRef: "PAGE-REF-1",
		Remark: "流水页断言用",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: variantID, SKUCode: sku, WarehouseID: warehouseID, Quantity: quantity,
		}},
	})
	if err != nil {
		t.Fatalf("变动失败: %v", err)
	}
}

func TestMovementsPageRendersSnapshotColumns(t *testing.T) {
	engine, f := newMovementsPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	stockID := seedStockRow(t, f, wh.ID, "TEA-500", "红茶 500ml", 18.5)
	changeStockOnRow(t, f, nil, v.ID, wh.ID, "TEA-500", inventoryenums.DirectionIn, "purchase_in", 10)

	if _, err := f.inventory.UpdateStock(context.Background(), &inventorydto.UpdateStockReq{
		ProjectID: f.projectID, ID: stockID, Name: "红茶 500ml（新包装）", CostPrice: floatPtr(987.65),
	}); err != nil {
		t.Fatalf("改主档失败: %v", err)
	}

	rec := httptestGet(engine, "/admin/inventory/movements?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"库存流水",

		"红茶 500ml",
		"TEA-500", "苏州仓", "SZ",

		"+10",

		"0 → 10",

		"18.5",

		"PAGE-REF-1", "inventory_page",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("流水页缺少 %q", want)
		}
	}
	if strings.Contains(page, "红茶 500ml（新包装）") {
		t.Fatalf("流水是审计：改主档不得改写历史里的货品名快照")
	}
	if strings.Contains(page, "987.65") {
		t.Fatalf("流水记录的是变动当时的单价（18.5），不该显示主档改后的成本")
	}
}

func TestMovementsPageFilters(t *testing.T) {
	engine, f := newMovementsPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	seedStockRow(t, f, sz.ID, "AAA-1", "甲货", 5)
	seedStockRow(t, f, sh.ID, "BBB-1", "乙货", 6)
	changeStockOnRow(t, f, nil, v.ID, sz.ID, "AAA-1", inventoryenums.DirectionIn, "purchase_in", 3)
	changeStockOnRow(t, f, nil, v.ID, sh.ID, "BBB-1", inventoryenums.DirectionIn, "purchase_in", 4)
	changeStockOnRow(t, f, nil, v.ID, sz.ID, "AAA-1", inventoryenums.DirectionOut, "sale_out", 1)

	rec := httptestGet(engine, "/admin/inventory/movements?project="+f.projectID+"&warehouseId="+sz.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("带仓筛应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "AAA-1") {
		t.Fatalf("按 SZ 筛后应有 AAA-1")
	}
	if strings.Contains(body, "BBB-1") {
		t.Fatalf("按 SZ 筛后不该出现 SH 的 BBB-1")
	}

	rec = httptestGet(engine, "/admin/inventory/movements?project="+f.projectID+"&direction=out")
	body = rec.Body.String()
	if !strings.Contains(body, "AAA-1") || strings.Contains(body, "BBB-1") {
		t.Fatalf("按 out 筛后应只剩 SZ 的出库那条")
	}

	rec = httptestGet(engine, "/admin/inventory/movements?project="+f.projectID+"&sku=BBB-1")
	body = rec.Body.String()
	if !strings.Contains(body, "BBB-1") || strings.Contains(body, "AAA-1") {
		t.Fatalf("按 SKU=BBB-1 筛后应只剩 BBB-1")
	}
}

func TestMovementsPageEmptyState(t *testing.T) {
	engine, f := newMovementsPageEngine(t)
	if engine == nil {
		return
	}
	rec := httptestGet(engine, "/admin/inventory/movements?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("空态页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "没有符合条件的库存流水") {
		t.Fatalf("空态页应有引导标题")
	}

	if !strings.Contains(body, "数量变化") {
		t.Fatalf("空态也要渲染表头")
	}
}
