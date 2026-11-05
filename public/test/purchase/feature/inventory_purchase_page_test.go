package feature

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	productdto "go_wp/internal/module/product/dto"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasehttp "go_wp/internal/module/purchase/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"
	"go_wp/public/migrations"
)

func newPurchasePageEngine(t *testing.T) (*gin.Engine, *purchaseFixture) {
	t.Helper()
	f := newPurchaseFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"inventory:purchase_create":     true,
			"inventory:purchase_production": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"inventory.purchase_create":     true,
			"inventory.purchase_production": true,
		})

	})
	handle := purchasehttp.NewPurchasePageHandle(f.purchase, f.stock, f.projects)
	handle.SetCatalog(f.catalog)
	engine.GET("/admin/inventory/purchases", pageview.Slot(purchasehttp.PurchasesPageTemplate, adminui.RenderPage), handle.PurchasesPage)
	engine.POST("/admin/inventory/purchases/create", pageview.HTMLFragment, handle.PurchaseCreate)
	engine.POST("/admin/inventory/purchases/receipt", pageview.HTMLFragment, handle.PurchaseReceipt)
	engine.POST("/admin/inventory/purchases/production", pageview.HTMLFragment, handle.PurchaseProduction)

	pageHandle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", pageview.Slot(inventoryhttp.InventoryListPageTemplate, adminui.RenderPage), pageHandle.InventoryPage)
	return engine, f
}

const purchaseLineSkuField = "lineSku"

func renderedLineSkuOption(t *testing.T, body, variantID string) string {
	t.Helper()
	marker := "<option value=\"" + variantID + "|"
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("采购页没有渲染变体 %s 的 SKU 选项值（模板字段名 / 值形状与 handler 对不上？）", variantID)
	}
	start := idx + len("<option value=\"")
	end := strings.Index(body[start:], "\"")
	if end < 0 {
		t.Fatalf("采购页的 SKU 选项值引号未闭合")
	}
	return body[start : start+end]
}

func seedPurchasePage(t *testing.T, f *purchaseFixture) (*inventorydto.WarehouseResp, *purchasedto.SourceResp,
	*purchasedto.SourceResp, *productdto.ProductResp, *productdto.VariantResp) {
	t.Helper()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	ext := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", purchaseenums.SourceTypeInternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	return wh, ext, factory, p, v
}

func TestPurchasePageRendersAndWrites(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	wh, ext, factory, p, v := seedPurchasePage(t, f)

	bare := bareSKU(v.SKUCode, wh.Code)

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"采购入库", "新建采购单",
		"name=\"csrf_token\"",
		"hx-post=\"/admin/inventory/purchases/create?",
		"hx-post=\"/admin/inventory/purchases/production?",
		"data-drawer-open=\"#tpl-purchase-create\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("采购入库页缺少 %s", want)
		}
	}

	invBody := httptestGet(engine, "/admin/inventory?project="+f.projectID).Body.String()
	for _, want := range []string{
		"生产入库", "/admin/inventory/purchases",
	} {
		if !strings.Contains(invBody, want) {
			t.Fatalf("库存页缺少「入库走单据」的边界提示 %s", want)
		}
	}
	if strings.Contains(invBody, "hx-post=\"/admin/inventory/production\"") {
		t.Fatalf("库存管理页不应再有内联的生产入库表单")
	}

	if !strings.Contains(body, "name=\""+purchaseLineSkuField+"\"") {
		t.Fatalf("新建采购单表单必须渲染出 handler 读取的 SKU 字段 %q", purchaseLineSkuField)
	}

	lineSku := renderedLineSkuOption(t, body, v.ID)
	if !strings.HasSuffix(lineSku, "|"+bare) {
		t.Fatalf("选项值应带上仓库侧裸码 %q，实际 %q", bare, lineSku)
	}

	form := url.Values{}
	form.Set("csrf_token", "test-csrf")
	form.Set("projectId", f.projectID)
	form.Set("code", "po-page-1")
	form.Set("sourceId", ext.ID)
	form.Set("warehouseId", wh.ID)
	form.Add(purchaseLineSkuField, lineSku)
	form.Add("lineQuantity", "4")
	form.Add("lineUnitPrice", "6.5")
	rec = postForm(engine, "/admin/inventory/purchases/create", form)

	assertInventoryJump(t, rec, "ok")
	orders, err := f.purchase.ListPurchaseOrders(ctx, &purchasedto.ListPurchaseOrderReq{ProjectID: f.projectID})
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

	if order.Lines[0].VariantID != v.ID || order.Lines[0].ProductID != p.ID || order.Lines[0].SKUCode != bare {
		t.Fatalf("采购行快照不正确（SKU 必须是仓库侧裸码 %q）：%+v", bare, order.Lines[0])
	}

	rec = postForm(engine, "/admin/inventory/purchases/create", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "code": {"po-page-empty"},
		"sourceId": {ext.ID}, "warehouseId": {wh.ID},
		"lineQuantity": {"1"}, "lineUnitPrice": {"1"},
	})
	assertInventoryJump(t, rec, "err", "采购单至少要有一行")
	if got, lerr := f.purchase.ListPurchaseOrders(ctx, &purchasedto.ListPurchaseOrderReq{ProjectID: f.projectID}); lerr != nil || len(got) != 1 {
		t.Fatalf("被拒绝的建单不应留下采购单：%v %+v", lerr, got)
	}

	rec = httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hx-post=\"/admin/inventory/purchases/receipt?") {
		t.Fatalf("有采购单后页面应渲染收货表单，实际 %d", rec.Code)
	}

	receiptForm := url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"4"}, "requestId": {"PAGE-RECV-1"},
	}
	rec = postForm(engine, "/admin/inventory/purchases/receipt", receiptForm)
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("表单收货后真源应为 4，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusReceived {
		t.Fatalf("收满后状态应为 received，实际 %q", got)
	}

	rec = postForm(engine, "/admin/inventory/purchases/receipt", receiptForm)
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("重复提交不应二次加库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("重复提交不应产生第二张入库单，实际 %d", got)
	}

	prodForm, _ := renderProductionForm(t, engine, f.projectID)
	if got := prodForm.Get("sourceId"); got != factory.ID {
		t.Fatalf("生产入库的来源应只列内部货源 %s，实际 %q", factory.ID, got)
	}
	prodForm.Set("quantity", "2")
	prodForm.Set("unitCost", "3.5")
	rec = postForm(engine, productionFormAction, prodForm)
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 6 {
		t.Fatalf("生产入库 2 后真源应为 6，实际 %d", got)
	}
	if got := variantCostIn(t, f, v.ID); got == nil || *got != 3.5 {
		t.Fatalf("生产入库应把手工成本价 3.5 写回 SKU，实际 %v", got)
	}

	rec = httptestGet(engine, "/admin/inventory?project="+f.projectID+"&sku="+url.QueryEscape(bare))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存流水页应 200，实际 %d", rec.Code)
	}
	body = rec.Body.String()
	for _, want := range []string{"PO-PAGE-1", "purchase_in", "production_in", bare} {
		if !strings.Contains(body, want) {
			t.Fatalf("库存流水缺少 %s", want)
		}
	}

	rec = postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"1"}, "requestId": {"PAGE-RECV-2"},
	})
	assertInventoryJump(t, rec, "err", "采购单已全部入库，无需再收")
}

func TestPurchasePageMultiDeviceContract(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, factory, p, v := seedPurchasePage(t, f)

	mustPurchaseOrder(t, f, "PO-MD-1", ext.ID, wh.ID, []purchasedto.PurchaseLineReq{purchaseLine(p, v, 3, 4.5)})
	cost := 2.0
	if _, err := f.purchase.RegisterProductionInbound(context.Background(), &purchasedto.ProductionInboundReq{
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
	if !strings.Contains(body, "stack purchase-page") {
		t.Fatalf("页面缺少多端适配的根类 purchase-page")
	}

	if !strings.Contains(body, `class="table-wrap table-scroll" tabindex="0"`) {
		t.Fatalf("采购单表应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}

	if !strings.Contains(body, "data-label=\"采购单号\"") || !strings.Contains(body, "data-label=\"操作\"") {
		t.Fatalf("表格单元应带 data-label（窄屏堆叠时回显列名）")
	}

	tplRaw, terr := os.ReadFile(purchaseAdminTemplatePath("purchases.html"))
	if terr != nil {
		t.Fatalf("读采购入库页模板失败: %v", terr)
	}
	if strings.Contains(string(tplRaw), "<script") {
		t.Fatalf("采购入库页模板不应引入自定义 JS（交互由原生表单完成）")
	}

	for _, want := range []string{"<input", "<select", "<button"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生控件 %s", want)
		}
	}

	fragRaw, ferr := os.ReadFile(purchaseAdminTemplatePath("purchase_create_form.html"))
	if ferr != nil {
		t.Fatalf("读采购新建片段模板失败: %v", ferr)
	}
	if got := strings.Count(string(tplRaw)+string(fragRaw), "name=\"csrf_token\""); got != 8 {
		t.Fatalf("八个写表单各要带一处 csrf_token 隐藏域（建单 / 收货 / 生产入库 / 下单 / 标异常 / 解除异常 / 提前关单 / 取消），页面+片段里共有 %d 处", got)
	}
	if got := strings.Count(string(tplRaw), "method=\"get\""); got != 2 {
		t.Fatalf("工程切换 + 采购单筛选都用 GET，模板里有 %d 处", got)
	}

	raw, err := os.ReadFile(templateRoot() + "/static/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)

	for _, want := range []string{
		".purchase-page-table",
		".purchase-page .form-inline .wbs",
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

func purchaseAdminTemplatePath(name string) string {
	return filepath.Join(templateRoot(), "admin", "purchase", name)
}

func TestPurchasePermissionsAndMenuSeeded(t *testing.T) {
	f := newPurchaseFixture(t)
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

	var apiPath, apiMethod string
	if err := f.db.Raw("SELECT api_path, api_method FROM sys_permission WHERE permission_code = 'inventory:purchase_receipt'").
		Row().Scan(&apiPath, &apiMethod); err != nil {
		t.Fatalf("查询权限点路径失败: %v", err)
	}
	if apiPath != "/api/inventory/purchase/receipt" || apiMethod != "POST" {
		t.Fatalf("权限点路径 / 方法不正确：%s %s", apiPath, apiMethod)
	}

	var menus int64
	if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND title = '采购入库' AND deleted_at IS NULL").
		Scan(&menus).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if menus != 1 {
		t.Fatalf("迁移 110 应 seed 「采购入库」后台菜单，实际 %d", menus)
	}
	for _, code := range []string{"inventory:purchase_create", "inventory:purchase_receipt", "inventory:purchase_production"} {
		var hit int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_menus WHERE type = 3 AND permission_code = ? AND deleted_at IS NULL", code).
			Scan(&hit).Error; err != nil {
			t.Fatalf("查询菜单按钮 %s 失败: %v", code, err)
		}
		if hit != 1 {
			t.Fatalf("采购按钮 %s 应已 seed，实际 %d 条", code, hit)
		}
	}

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
