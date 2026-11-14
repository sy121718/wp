package feature

import (
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	pkgcasbin "go_wp/pkg/casbin"
)

func newExternalSKUEditEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们片段渲染为空。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"inventory:stock_change": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"inventory.stock_change": true})
	})
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", pageview.Slot(inventoryhttp.InventoryListPageTemplate, pageview.RenderPage), handle.InventoryPage)
	engine.POST("/admin/inventory/external-sku", pageview.HTMLFragment, handle.InventoryExternalSKUUpdate)
	return engine, f
}

func seedExternalSKUEditRow(t *testing.T, f *invFixture) (*inventorydto.WarehouseResp,
	*productdto.ProductResp, *productdto.VariantResp) {
	t.Helper()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProductPriced(t, f, "外码编辑商品", 9.9)
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 7, "purchase_in")
	if err := f.db.Exec("UPDATE inventory_stocks SET cost_price = ? WHERE variant_id = ? AND warehouse_id = ?",
		12.50, v.ID, wh.ID).Error; err != nil {
		t.Fatalf("铺仓库侧成本价失败: %v", err)
	}
	return wh, p, v
}

func postExternalSKU(engine *gin.Engine, projectID, warehouseID, variantID, skuCode, externalSKU string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("csrf_token", "test-csrf")
	form.Set("projectId", projectID)
	form.Set("warehouseId", warehouseID)
	form.Set("variantId", variantID)
	form.Set("skuCode", skuCode)
	form.Set("externalSku", externalSKU)
	path := "/admin/inventory/external-sku?project=" + url.QueryEscape(projectID) + "&sku=" + url.QueryEscape(skuCode)
	return postForm(engine, path, form)
}

func externalSKUPageBody(t *testing.T, engine *gin.Engine, projectID, sku, extraQuery string) string {
	t.Helper()
	path := "/admin/inventory?project=" + url.QueryEscape(projectID) + "&sku=" + url.QueryEscape(sku)
	if extraQuery != "" {
		path += "&" + extraQuery
	}
	rec := httptestGet(engine, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("库存页应 200，实际 %d", rec.Code)
	}
	return rec.Body.String()
}

type stockRowSnapshot struct {
	SKUCode  string
	Quantity int
	Cost     *float64
}

func snapshotStockRow(t *testing.T, f *invFixture, variantID, warehouseID string) stockRowSnapshot {
	t.Helper()
	var row struct {
		SKUCode  string
		Quantity int
	}
	if err := f.db.Raw("SELECT sku_code, quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&row).Error; err != nil {
		t.Fatalf("读库存行快照失败: %v", err)
	}
	return stockRowSnapshot{
		SKUCode: row.SKUCode, Quantity: row.Quantity,
		Cost: stockCostIn(t, f, variantID, warehouseID),
	}
}

func sameCostPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestExternalSKUEditSavesAndRenders(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, v := seedExternalSKUEditRow(t, f)

	bare := bareSKU(v.SKUCode, wh.Code)

	rec := postExternalSKU(engine, f.projectID, wh.ID, v.ID, bare, "EXT-EDIT-1")

	assertInventoryJump(t, rec, "ok")
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "EXT-EDIT-1" {
		t.Fatalf("真源外码应写为 EXT-EDIT-1，实际 %q", got)
	}

	body := externalSKUPageBody(t, engine, f.projectID, bare, "")
	if !strings.Contains(body, "action=\"/admin/inventory/external-sku?") {
		t.Fatalf("库存页应渲染外部编码的行内编辑表单（action 缺失）")
	}
	if !strings.Contains(body, "name=\"externalSku\"") || !strings.Contains(body, "value=\"EXT-EDIT-1\"") {
		t.Fatalf("库存页应把新外码回显到行内输入框里")
	}

	for _, want := range []string{
		"name=\"variantId\" value=\"" + v.ID + "\"",
		"name=\"warehouseId\" value=\"" + wh.ID + "\"",
		"name=\"csrf_token\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("行内编辑表单缺少 %s", want)
		}
	}
}

func TestExternalSKUEditRejectsInvalidCode(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, v := seedExternalSKUEditRow(t, f)

	cases := []struct {
		name string
		code string
	}{
		{"超长（129 字符）", strings.Repeat("X", 129)},
		{"含控制字符（换行）", "EXT-\n-2"},
		{"含控制字符（TAB）", "EXT-\t3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, tc.code)

			assertInventoryJump(t, rec, "err", "外部编码不合法")
			if strings.Contains(rec.Body.String(), "ErrExternalSKUInvalid") {
				t.Fatalf("提示页不该出现裸 i18n key：%s", rec.Body.String())
			}

			if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
				t.Fatalf("非法外码不该落库，实际 %q", got)
			}
		})
	}
}

func TestExternalSKUEditCrossProductConflict(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, va := seedExternalSKUEditRow(t, f)

	pb := mustProductPriced(t, f, "冲突商品", 19.9)
	vb := f.firstVariant(t, pb.ID)

	assertInventoryJump(t, postExternalSKU(engine, f.projectID, wh.ID, va.ID, va.SKUCode, "EXT-SHARED"), "ok")

	rec := postExternalSKU(engine, f.projectID, wh.ID, vb.ID, vb.SKUCode, "EXT-SHARED")
	assertInventoryJump(t, rec, "err", "该外部编码在本仓已挂在另一个商品上")
	if strings.Contains(rec.Body.String(), "ErrExternalSKUProductConflict") {
		t.Fatalf("提示页不该出现裸 i18n key：%s", rec.Body.String())
	}

	if got := externalSKUOf(t, f, vb.ID, wh.ID); got != "" {
		t.Fatalf("冲突时真源不该被写，实际 %q", got)
	}
	if got := externalSKUOf(t, f, va.ID, wh.ID); got != "EXT-SHARED" {
		t.Fatalf("冲突不该影响已占位那一行，实际 %q", got)
	}
}

func TestExternalSKUEditClearOnlyTouchesExternalCode(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, v := seedExternalSKUEditRow(t, f)

	assertInventoryJump(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-CLEAR"), "ok")
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "EXT-CLEAR" {
		t.Fatalf("登记后真源外码应为 EXT-CLEAR，实际 %q", got)
	}

	before := snapshotStockRow(t, f, v.ID, wh.ID)
	if before.Quantity != 7 {
		t.Fatalf("前置数量应为 7，实际 %d", before.Quantity)
	}
	if before.Cost == nil || *before.Cost != 12.50 {
		t.Fatalf("前置成本应为 12.50，实际 %v", before.Cost)
	}

	assertInventoryJump(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, ""), "ok")
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
		t.Fatalf("清空后真源外码应为空串，实际 %q", got)
	}

	after := snapshotStockRow(t, f, v.ID, wh.ID)
	if after.SKUCode != before.SKUCode {
		t.Fatalf("清空外码动了 sku_code：%q → %q", before.SKUCode, after.SKUCode)
	}
	if after.Quantity != before.Quantity {
		t.Fatalf("清空外码动了数量：%d → %d", before.Quantity, after.Quantity)
	}
	if !sameCostPtr(before.Cost, after.Cost) {
		t.Fatalf("清空外码动了成本：%v → %v", before.Cost, after.Cost)
	}

	body := externalSKUPageBody(t, engine, f.projectID, v.SKUCode, "")
	if strings.Contains(body, "value=\"EXT-CLEAR\"") {
		t.Fatalf("清空后页面不该再回显旧外码")
	}
	if !strings.Contains(body, "name=\"externalSku\"") {
		t.Fatalf("清空后行内编辑输入框应仍在（空 = 该仓用我们自己的 SKU）")
	}

	assertInventoryJump(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-AGAIN"), "ok")
	assertInventoryJump(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "   "), "ok")
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
		t.Fatalf("纯空白提交后真源外码应为空串，实际 %q", got)
	}
}

func TestExternalSKUEditUnauthorizedBlocked(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh, _, v := seedExternalSKUEditRow(t, f)
	if err := pkgcasbin.InitCasbin(f.db); err != nil {
		t.Fatalf("初始化 Casbin（隔离库）失败: %v", err)
	}
	t.Cleanup(func() { _ = pkgcasbin.Close() })

	gin.SetMode(gin.TestMode)
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	newEngine := func(userID any) *gin.Engine {
		e := gin.New()
		e.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
		// 生产装配在基础层挂成功/失败出口与请求取词；缺了它们写片段渲染为空。
		e.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
		e.Use(pageview.FailureMiddleware(pageview.RenderFailure))
		e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
		if userID != nil {
			uid := userID
			e.Use(func(c *gin.Context) { c.Set("user_id", uid) })
		}
		e.POST("/admin/inventory/external-sku", pageview.HTMLFragment,
			builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), handle.InventoryExternalSKUUpdate)
		return e
	}

	rec := postExternalSKU(newEngine(nil), f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-DENIED-1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录提交应 401，实际 %d：%s", rec.Code, rec.Body.String())
	}

	const deniedUser int64 = 4242
	rec = postExternalSKU(newEngine(deniedUser), f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-DENIED-2")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限提交应 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
		t.Fatalf("被拦下的请求不该改动真源，实际外码 %q", got)
	}

	enforcer := pkgcasbin.GetEnforcer()
	if enforcer == nil {
		t.Fatal("Casbin enforcer 未就绪")
	}
	sub := strconv.FormatInt(deniedUser, 10)
	if _, err := enforcer.AddPolicy(sub, "/api/inventory/stock/change", "POST", "inventory:stock_change"); err != nil {
		t.Fatalf("授予 inventory:stock_change 失败: %v", err)
	}
	rec = postExternalSKU(newEngine(deniedUser), f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-ALLOWED")

	assertInventoryJump(t, rec, "ok")
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "EXT-ALLOWED" {
		t.Fatalf("放行后真源外码应为 EXT-ALLOWED，实际 %q", got)
	}

	src, err := os.ReadFile("../../../../internal/module/inventory/inbound/http/inventory_page_router.go")
	if err != nil {
		t.Fatalf("读库存路由装配失败: %v", err)
	}
	want := `pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change")`
	if !strings.Contains(string(src), want) {
		t.Fatalf("页面路由 /admin/inventory/external-sku 没有按既有权限点 inventory:stock_change 挂中间件")
	}
}

func TestExternalSKUEditI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	for _, key := range []string{
		"admin.inventory.sku.externalSku.ph",
		"admin.inventory.sku.externalSku.save",
		"admin.inventory.sku.externalSku.actionHint",
	} {
		var count int
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", key).Scan(&count).Error; err != nil {
			t.Fatalf("查词条 %s 失败: %v", key, err)
		}
		if count != 2 {
			t.Fatalf("词条 %s 应有中英各一行（迁移 255），实际 %d 行", key, count)
		}
	}

	var value string
	if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'",
		"admin.inventory.sku.externalSku.save").Scan(&value).Error; err != nil {
		t.Fatalf("读中文词条失败: %v", err)
	}
	if strings.TrimSpace(value) == "" || strings.Contains(value, "admin.inventory") {
		t.Fatalf("中文词条应是一条真文案，实际 %q", value)
	}
}
