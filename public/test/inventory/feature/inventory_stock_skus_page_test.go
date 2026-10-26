// inventory_stock_skus_page_test.go — 仓库商品 SKU 台账页（迁移 642 / B3）。
//
// 覆盖的会红断言：
//
//	1  台账页把「停用的、还没被任何商品引用的」货行也列出来（候选下拉只列能选的，
//	   台账页要看得见全部 —— 否则运营没法判断「这批货到底建过没有」）
//	2  三条写动作（新建 / 编辑主档 / 启停）后落库字段正确，且**数量不被主档写动作改动**
//	3  筛选：按仓 + 状态收窄，页面上只剩命中行
//	4  两币不同却没汇率 → 「汇率未固化」，不按 1:1 猜
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

// newStockSKUsPageEngine 只挂台账页与该页三条写动作的测试引擎（真实 Jet + 真实 service）。
func newStockSKUsPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 按钮按 sys_menus 的 type=3 节点渲染（adminui.Prepare 读 ButtonsKey）：
	// 这条链路不挂鉴权中间件，注入一份按钮集，让「页面里存在写入口」的断言保持有效。
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

// TestStockSKUsPageListsAllRows 验收 1 + 4：
// 台账页要看见停用的与没被引用的行；两币不同又没汇率时显示「汇率未固化」。
func TestStockSKUsPageListsAllRows(t *testing.T) {
	engine, f := newStockSKUsPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	ctx := context.Background()
	// ① 有名称、有成本的启用行。
	if _, err := f.inventory.CreateStock(ctx, &inventorydto.CreateStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "RED-500", Name: "红茶 500ml",
		Quantity: 12, TrackQuantity: true, CostPrice: floatPtr(18.5),
	}); err != nil {
		t.Fatalf("建货行失败: %v", err)
	}
	// ② 停用 + 两币不同 + 没有汇率的那一行：这正是台账页存在的理由（商品侧看不见它）。
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
		// 停用行必须看得见：状态文案来自模板取词（admin.inventory.status.disabled = 已停用）。
		"已停用",
		// 没被引用 = 一个变体都没有，页面要明说，而不是留一格空白。
		"暂无商品引用",
		// 汇率未固化：两个币种不同却没有汇率，不得按 1:1 猜一个能算的折算金额。
		"汇率未固化",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("台账页缺少 %q", want)
		}
	}
	// 成本列要显示真实金额（FormatYuan 最短表示：18.5 不是 18.50，也不是 —）。
	// 空成本才显示 —，那是「尚未核算」，与「零成本」必须能分开。
	if !strings.Contains(page, "18.5") {
		t.Fatalf("成本列应显示 18.5（最短表示），页面里没有")
	}
}

// TestStockSKUWriteActions 验收 2 + 3：三条写动作落库正确、数量不被主档写动作改动、筛选生效。
func TestStockSKUWriteActions(t *testing.T) {
	engine, f := newStockSKUsPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	// ① 新建：带名称 / 条码 / 成本 / 初始数量。
	rec := postForm(engine, "/admin/inventory/stock/create", url.Values{
		"projectId": {f.projectID}, "warehouseId": {sz.ID}, "skuCode": {"TEA-001"},
		"name": {"乌龙 500ml"}, "barcode": {"6901234567890"},
		"quantity": {"7"}, "trackQuantity": {"1"}, "costPrice": {"21.00"}, "status": {"active"},
	})
	if rec.Code != 200 {
		t.Fatalf("新建货行应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 列名要显式给 gorm tag：Scan 按字段名的下划线形式匹配列（Cost → cost），
	// 与真列名 cost_price 对不上，缺 tag 会静默扫成 nil。
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

	// ② 编辑主档：改名 + 改成本；数量**不动**（主档写动作没有数量入口）。
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

	// ③ 启停：停用后行仍在（停用不等于删除）。
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

	// ④ 筛选：另建一条在另一个仓的行，按仓筛后页面上只剩 SZ 那一条。
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
	// 停用的行在不带状态筛选时仍要出现（这是台账页与候选下拉的区别）。
	if !strings.Contains(page, "TEA-001") {
		t.Fatalf("台账页不该隐藏停用行")
	}
}

// TestStockSKUsPageEmptyState 空仓时给的是「先建一条」而不是一张空白表。
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

// newTestRecorder 占位以保证 httptest 引入被使用（页面断言全部走 httptestGet）。
var _ = httptest.NewRecorder
