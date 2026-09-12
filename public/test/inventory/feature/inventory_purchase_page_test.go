// Package feature 采购入库后台页的写链路、多端适配与权限 seed（issue #18）。
//
// 后台页是服务端渲染的原生表单页，因此这里覆盖三件事：
//
//	· 写链路：GET 渲染 + POST 原生表单（csrf_token 隐藏域）→ 单真的建出来、货真的收进来、
//	  重复提交真的被幂等键挡住；
//	· 多端 / 多输入契约：只用原生控件、宽表包在可聚焦的滚动容器里、样式表按断点改堆叠块
//	  （模板里的 data-label 在堆叠态由 CSS ::before 回显列名）；
//	· 迁移 108/109/110：采购权限点与后台菜单已 seed（未 seed 时 Casbin 无策略 → 全员 403）。
package feature

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	"go_wp/internal/templates"

	"go_wp/public/migrations"
)

// newPurchasePageEngine 装配只挂采购入库页的测试引擎（真实 Jet 模板 + 真实 service）。
func newPurchasePageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	handle := dashboardhttp.NewInventoryPurchasePageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory/purchases", handle.InventoryPurchasesPage)
	engine.POST("/admin/inventory/purchases/create", handle.InventoryPurchaseCreate)
	engine.POST("/admin/inventory/purchases/receipt", handle.InventoryPurchaseReceipt)
	engine.POST("/admin/inventory/purchases/production", handle.InventoryPurchaseProduction)
	return engine, f
}

// seedPurchasePage 铺页面所需数据：默认仓 + 外部货源 + 内部货源 + 商品（含首个变体）。
func seedPurchasePage(t *testing.T, f *invFixture) (*inventorydto.WarehouseResp, *inventorydto.SourceResp,
	*inventorydto.SourceResp, *productdto.ProductResp, *productdto.VariantResp) {
	t.Helper()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	ext := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", inventoryenums.SourceTypeInternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	return wh, ext, factory, p, v
}

// TestPurchasePageRendersAndWrites 后台页可建单、可收货、可生产入库、可查进货历史。
func TestPurchasePageRendersAndWrites(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	wh, ext, factory, p, v := seedPurchasePage(t, f)

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	// 空白态：建单表单与生产入库表单都在（收货表单要等有采购单才会出现）。
	for _, want := range []string{
		"采购入库", "新建采购单", "生产入库（自家工厂）", "进货历史",
		"name=\"csrf_token\"",
		"action=\"/admin/inventory/purchases/create\"",
		"action=\"/admin/inventory/purchases/production\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("采购入库页缺少 %s", want)
		}
	}

	// 原生表单建采购单（一行）。
	form := url.Values{}
	form.Set("csrf_token", "test-csrf")
	form.Set("projectId", f.projectID)
	form.Set("code", "po-page-1")
	form.Set("sourceId", ext.ID)
	form.Set("warehouseId", wh.ID)
	form.Add("lineVariantId", v.ID)
	form.Add("lineProductId", p.ID)
	form.Add("lineSKUCode", v.SKUCode)
	form.Add("lineQuantity", "4")
	form.Add("lineUnitPrice", "6.5")
	rec = postForm(engine, "/admin/inventory/purchases/create", form)
	if rec.Code != http.StatusFound {
		t.Fatalf("建单表单应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	orders, err := f.inventory.ListPurchaseOrders(ctx, &inventorydto.ListPurchaseOrderReq{ProjectID: f.projectID})
	if err != nil || len(orders) != 1 {
		t.Fatalf("表单应建出一张采购单：%v %+v", err, orders)
	}
	order := orders[0]
	if order.Code != "PO-PAGE-1" || len(order.Lines) != 1 || order.Lines[0].Quantity != 4 {
		t.Fatalf("表单建单结果不正确：%+v", order)
	}
	if order.Lines[0].UnitPrice != 6.5 {
		t.Fatalf("表单单价未落库：%+v", order.Lines[0])
	}

	// 有单据后重新渲染：每一行都带「登记入库」原生表单（幂等键来自页面的隐藏域）。
	rec = httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "action=\"/admin/inventory/purchases/receipt\"") {
		t.Fatalf("有采购单后页面应渲染收货表单，实际 %d", rec.Code)
	}

	// 收货表单（幂等键来自页面的隐藏域）。
	receiptForm := url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"4"}, "requestId": {"PAGE-RECV-1"},
	}
	rec = postForm(engine, "/admin/inventory/purchases/receipt", receiptForm)
	if rec.Code != http.StatusFound {
		t.Fatalf("收货表单应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("表单收货后真源应为 4，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusReceived {
		t.Fatalf("收满后状态应为 received，实际 %q", got)
	}
	// 同一份表单被重复提交（浏览器双击 / 回退重发）：幂等键挡住，库存只加一次。
	rec = postForm(engine, "/admin/inventory/purchases/receipt", receiptForm)
	if rec.Code != http.StatusFound {
		t.Fatalf("重复提交应安静返回列表（幂等命中），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("重复提交不应二次加库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("重复提交不应产生第二张入库单，实际 %d", got)
	}

	// 生产入库表单：无采购单 + 成本价手工填写。
	rec = postForm(engine, "/admin/inventory/purchases/production", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "sourceId": {factory.ID},
		"variantId": {v.ID}, "productId": {p.ID}, "skuCode": {v.SKUCode},
		"warehouseId": {wh.ID}, "quantity": {"2"}, "unitCost": {"3.5"}, "requestId": {"PAGE-PROD-1"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("生产入库表单应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 6 {
		t.Fatalf("生产入库 2 后真源应为 6，实际 %d", got)
	}
	if got := variantCostIn(t, f, v.ID); got == nil || *got != 3.5 {
		t.Fatalf("生产入库应把手工成本价 3.5 写回 SKU，实际 %v", got)
	}

	// 进货历史按 SKU 过滤：采购收货与生产入库都在同一张历史里。
	rec = httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID+"&sku="+url.QueryEscape(v.SKUCode))
	if rec.Code != http.StatusOK {
		t.Fatalf("进货历史页应 200，实际 %d", rec.Code)
	}
	body = rec.Body.String()
	for _, want := range []string{"PO-PAGE-1", "采购收货", "生产入库", v.SKUCode} {
		if !strings.Contains(body, want) {
			t.Fatalf("进货历史缺少 %s", want)
		}
	}

	// 业务错误经 ?err= 回显（这里已收满的单再收一次）。
	rec = postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"1"}, "requestId": {"PAGE-RECV-2"},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err="+inventoryenums.ErrReceiptOrderDone) {
		t.Fatalf("业务错误应经 ?err= 回显，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// TestPurchasePageMultiDeviceContract 多端 / 多输入适配的结构契约。
func TestPurchasePageMultiDeviceContract(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, factory, p, v := seedPurchasePage(t, f)
	// 铺一张未收货的采购单（页面上因此有「登记入库」表单）与一条生产入库历史（有历史表）。
	mustPurchaseOrder(t, f, "PO-MD-1", ext.ID, wh.ID, []inventorydto.PurchaseLineReq{purchaseLine(p, v, 3, 4.5)})
	cost := 2.0
	if _, err := f.inventory.RegisterProductionInbound(context.Background(), &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 1, UnitCost: &cost, RequestID: "MD-PROD-1",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "pages-wrap purchase-page") {
		t.Fatalf("页面缺少多端适配的根类 purchase-page")
	}
	// 键盘可滚的表格容器（宽表在窄视口下靠它横向滚动，且能 Tab 聚焦后用方向键滚）。
	// 容器类由 pages-table-wrap 迁到公共类 table-wrap（滚动与键盘聚焦都在 .table-wrap 上）。
	if !strings.Contains(body, "table-wrap\" tabindex=\"0\"") {
		t.Fatalf("采购行 / 进货历史表应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}
	// 窄屏堆叠态靠 data-label 回显列名（列名不随表头消失而丢失）。
	if !strings.Contains(body, "data-label=\"登记入库\"") || !strings.Contains(body, "data-label=\"单价\"") {
		t.Fatalf("表格单元应带 data-label（窄屏堆叠时回显列名）")
	}
	// 模板不含任何 <script>：交互全由原生表单提交完成（layout 里的全局壳不算本页引入）。
	tplRaw, terr := os.ReadFile(templateRoot() + "/admin/inventory_purchases.html")
	if terr != nil {
		t.Fatalf("读采购入库页模板失败: %v", terr)
	}
	if strings.Contains(string(tplRaw), "<script") {
		t.Fatalf("采购入库页模板不应引入自定义 JS（交互由原生表单完成）")
	}
	// 四种输入都落在原生控件上。
	for _, want := range []string{"<input", "<select", "<button"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生控件 %s", want)
		}
	}
	// 每个写表单（POST）都带 csrf_token 隐藏域（原生表单的 CSRF 契约）：
	// 建单 / 收货 / 生产入库三个写入口各一处；GET 的筛选与工程切换表单不带。
	if got := strings.Count(string(tplRaw), "name=\"csrf_token\""); got != 3 {
		t.Fatalf("三个写表单各要带一处 csrf_token 隐藏域，模板里有 %d 处", got)
	}
	if got := strings.Count(string(tplRaw), "method=\"get\""); got != 3 {
		t.Fatalf("工程切换 + 采购单筛选 + 进货历史筛选都用 GET，模板里有 %d 处", got)
	}

	// 样式表契约：断点内的堆叠块 + 宽度 min(100%, …) 收口 + 输入不写死像素。
	raw, err := os.ReadFile(templateRoot() + "/static/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)
	for _, want := range []string{
		".purchase-page-table",
		".purchase-page .attr-form-head .wbs",
		".purchase-page .receipt-form",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式表缺少采购入库页规则 %s", want)
		}
	}
	idx := strings.Index(css, "手机：表格改堆叠块")
	if idx < 0 {
		t.Fatalf("样式表缺少采购入库页窄屏适配段的说明")
	}
	block := css[idx:]
	if len(block) > 700 {
		block = block[:700]
	}
	if !strings.Contains(block, "max-width: 720px") || !strings.Contains(block, ".purchase-page-table thead { display: none; }") {
		t.Fatalf("窄屏断点缺少采购表格的堆叠规则：%s", block)
	}
	if !strings.Contains(css, "content: attr(data-label)") {
		t.Fatalf("窄屏堆叠态应用 data-label 回显列名")
	}
	if !strings.Contains(css, "width: min(100%, 120px)") && !strings.Contains(css, "width: min(100%, 220px)") {
		t.Fatalf("输入宽度应按 min(100%%, <设计宽度>) 收口，不能写死像素")
	}
}

// TestPurchasePermissionsAndMenuSeeded 迁移 108/109/110：
// 采购权限点与后台菜单已 seed（未 seed 时 Casbin 无策略 → 含超管全员 403）。
func TestPurchasePermissionsAndMenuSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	codes := []string{
		"inventory:purchase_list", "inventory:purchase_get", "inventory:purchase_create",
		"inventory:purchase_update", "inventory:purchase_receipt",
		"inventory:purchase_production", "inventory:purchase_history",
	}
	for _, code := range codes {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_permission WHERE permission_code = ?", code).Scan(&hit).Error; err != nil {
			t.Fatalf("查询权限点 %s 失败: %v", code, err)
		}
		if hit != 1 {
			t.Fatalf("权限点 %s 应已 seed，实际 %d 条", code, hit)
		}
	}
	// 权限点已挂路径 / 方法（Casbin 按 path+method 匹配）。
	var apiPath, apiMethod string
	if err := f.db.Raw("SELECT api_path, api_method FROM sys_permission WHERE permission_code = 'inventory:purchase_receipt'").
		Row().Scan(&apiPath, &apiMethod); err != nil {
		t.Fatalf("查询权限点路径失败: %v", err)
	}
	if apiPath != "/api/inventory/purchase/receipt" || apiMethod != "POST" {
		t.Fatalf("权限点路径 / 方法不正确：%s %s", apiPath, apiMethod)
	}
	// 后台菜单 + 按钮。
	var menus int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND title = '采购入库' AND deleted_time IS NULL").
		Scan(&menus).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if menus != 1 {
		t.Fatalf("迁移 110 应 seed 「采购入库」后台菜单，实际 %d", menus)
	}
	for _, code := range []string{"inventory:purchase_create", "inventory:purchase_receipt", "inventory:purchase_production"} {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 3 AND permission_code = ? AND deleted_time IS NULL", code).
			Scan(&hit).Error; err != nil {
			t.Fatalf("查询菜单按钮 %s 失败: %v", code, err)
		}
		if hit != 1 {
			t.Fatalf("采购按钮 %s 应已 seed，实际 %d 条", code, hit)
		}
	}
	// 四张新表都在（迁移 108）。
	for _, table := range []string{
		"inventory_purchase_orders", "inventory_purchase_order_lines",
		"inventory_purchase_receipts", "inventory_purchase_receipt_items",
	} {
		var exists int64
		if err := f.db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?", table).
			Scan(&exists).Error; err != nil {
			t.Fatalf("查询表 %s 失败: %v", table, err)
		}
		if exists != 1 {
			t.Fatalf("迁移 108 应建出表 %s", table)
		}
	}
}
