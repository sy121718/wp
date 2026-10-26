// inventory_movements_page_test.go — 库存流水独立页（B4）。
//
// 覆盖的会红断言：
//
//	1  流水页把一条变动完整呈现：货品名 / 编码 / 仓库 / 方向 / 变动符号 / 数量变化 /
//	   原因 / 来源引用 / 单价 —— 少任何一列，这条记录事后都解释不清
//	2  货品名与单价是**变动当时的快照**：之后改主档不改写历史（流水是审计）
//	3  筛选取的是流水的维度（方向 / SKU / 仓），与库存管理页的「按 SKU 看当前数量」不同
//	4  空态给的是「还没有变动过」而不是一张空白表
package feature

import (
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

// newMovementsPageEngine 只挂流水页的测试引擎（真实 Jet + 真实 service）。
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
		adminui.PageSlot(inventoryhttp.InventoryMovementsPageTemplate), handle.InventoryMovementsPage)
	return engine, f
}

// seedStockRow 建一条带主档的货行（名称 / 成本都填），返回它的 id。
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

// changeStockOnRow 在指定货行上做一次变动（编码传仓库侧裸码，服务端按 (仓, 编码) 定位）。
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

// TestMovementsPageRendersSnapshotColumns 验收 1 + 2：
// 一条流水的全部事实都要在页面上，且货品名 / 单价取变动当时的快照。
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

	// 主档改名换成本：流水里的快照**不该**跟着变（这就是快照列存在的理由）。
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
		// 快照：改名之后页面里仍是变动当时的名称。
		"红茶 500ml",
		"TEA-500", "苏州仓", "SZ",
		// 入库方向与带符号的变动量（符号由 service 给，模板不做算术）。
		"+10",
		// 数量变化是 from → to。
		"0 → 10",
		// 单价是变动当时的成本（18.5），不是主档改后的 99。
		"18.5",
		// 来源引用与原因都在。
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

// TestMovementsPageFilters 验收 3：按方向 / SKU / 仓收窄，页面上只剩命中行。
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

	// 按仓筛：只留 SZ 的两条（甲货的入与出）。
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

	// 按方向筛：out 只剩那一条出库。
	rec = httptestGet(engine, "/admin/inventory/movements?project="+f.projectID+"&direction=out")
	body = rec.Body.String()
	if !strings.Contains(body, "AAA-1") || strings.Contains(body, "BBB-1") {
		t.Fatalf("按 out 筛后应只剩 SZ 的出库那条")
	}

	// 按 SKU 筛：BBB-1 只剩它自己的入库。
	rec = httptestGet(engine, "/admin/inventory/movements?project="+f.projectID+"&sku=BBB-1")
	body = rec.Body.String()
	if !strings.Contains(body, "BBB-1") || strings.Contains(body, "AAA-1") {
		t.Fatalf("按 SKU=BBB-1 筛后应只剩 BBB-1")
	}
}

// TestMovementsPageEmptyState 验收 4：没有变动时给引导而不是空白表（表头仍要在）。
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
	// 表头常驻：空数据不该连「这里能看什么」一起藏掉。
	if !strings.Contains(body, "数量变化") {
		t.Fatalf("空态也要渲染表头")
	}
}
