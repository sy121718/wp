// Package feature inventory 模块 feature 测试 —— 库存页「外部编码」的行内编辑入口。
//
// 背景：迁移 251 给 inventory_stocks 加了 external_sku，但库存页当时只有**只读列** ——
// 早于 251 建的老商品事后没有地方登记外码。本批在库存页那一列加上行内编辑
// （原生表单 POST + PRG 回列表），本文件覆盖它的六条完成判据：
//
//  1. 写入合法外码成功：302 回列表带 done=1，且新值真的渲染回页面；
//  2. 非法外码（超长 / 控制字符）被拒：页面上是**中文**，真源一字未写；
//  3. 跨商品冲突被拒：页面上是**中文**，真源一字未写；
//  4. 清空外码成功（空串 = 撤销映射），页面不再回显旧值；
//  5. 清空只动 external_sku：sku_code / quantity / cost_price 逐项不变；
//  6. 未授权路径仍按既有权限点拦截（401 / 403 / 授权后放行）。
//
// 断言一律直查真源列（inventory_stocks.external_sku / sku_code / quantity / cost_price），
// 不走 service 自己返回的响应 —— 那只能证明「service 以为自己写了」。
package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
	pkgcasbin "go_wp/pkg/casbin"
)

// newExternalSKUEditEngine 装配只挂库存页与「外码行内编辑」写入口的测试引擎（真实 Jet + 真实 service）。
//
// 与生产装配同形：写入口是原生表单 POST + PRG（与采购页的「登记入库」同一手法），
// 不入 HTMX、不写自定义 JS。这里注入的权限集合只供**渲染**（shell.Prepare 读 PermSetKey），
// 不参与拦截 —— 拦截一律由路由上的 Casbin 中间件判定，见 TestExternalSKUEditUnauthorizedBlocked。
func newExternalSKUEditEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{"inventory:stock_change": true})
	})
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", handle.InventoryPage)
	engine.POST("/admin/inventory/external-sku", handle.InventoryExternalSKUUpdate)
	return engine, f
}

// seedExternalSKUEditRow 铺一行「有数量、有成本」的库存：数量走真实变动契约，
// 成本直改真源（成本在这里只用于「外码编辑不许把它带走」的比对）。
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

// postExternalSKU 提交行内编辑表单（字段与模板里那份表单一一对应）。
func postExternalSKU(engine *gin.Engine, projectID, warehouseID, variantID, skuCode, externalSKU string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("csrf_token", "test-csrf")
	form.Set("projectId", projectID)
	form.Set("warehouseId", warehouseID)
	form.Set("variantId", variantID)
	form.Set("skuCode", skuCode)
	form.Set("externalSku", externalSKU)
	return postForm(engine, "/admin/inventory/external-sku", form)
}

// externalSKURedirect 断言「302 回列表（PRG）」并返回 Location。
func externalSKURedirect(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("外码表单应 302 回列表（PRG），实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
}

// errTextOf 取回跳地址里的 err 参数（页面上的中文错误文案）。
func errTextOf(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("回跳地址无法解析：%q", location)
	}
	return u.Query().Get("err")
}

// externalSKUPageBody 渲染库存页某 SKU 的各仓库存（写入口的回显证据都在这一段里）。
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

// stockRowSnapshot 一行库存的三列快照（清空外码时的「逐项不变」基线）。
type stockRowSnapshot struct {
	SKUCode  string
	Quantity int
	Cost     *float64
}

// snapshotStockRow 直读真源三列（不经过任何 service 投影）。
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

// sameCostPtr 两个可空成本是否相等（nil 与 nil 相等；nil 与 0 不相等）。
func sameCostPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// TestExternalSKUEditSavesAndRenders 判据 1：合法外码写入成功、PRG 回列表、页面回显新值。
func TestExternalSKUEditSavesAndRenders(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, v := seedExternalSKUEditRow(t, f)
	// 库存页的 SKU 上下文是**仓库侧裸码**（页面表单 hidden 域与筛选框都是它，
	// 库存行 / 流水存的也是它）；商品 / 变体侧的 v.SKUCode 仍带仓码前缀。
	bare := bareSKU(v.SKUCode, wh.Code)

	loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, bare, "EXT-EDIT-1"))
	if !strings.Contains(loc, "done=1") {
		t.Fatalf("成功回跳应带回 done=1，实际 %q", loc)
	}
	if !strings.Contains(loc, "project="+f.projectID) || !strings.Contains(loc, "sku="+url.QueryEscape(bare)) {
		t.Fatalf("成功回跳应保留工程与当前 SKU 上下文（裸码 %q），实际 %q", bare, loc)
	}
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "EXT-EDIT-1" {
		t.Fatalf("真源外码应写为 EXT-EDIT-1，实际 %q", got)
	}

	// 回列表：新值必须真的渲染进那一列的输入框（而不只是躺在库里）。
	body := externalSKUPageBody(t, engine, f.projectID, bare, "")
	if !strings.Contains(body, "action=\"/admin/inventory/external-sku\"") {
		t.Fatalf("库存页应渲染外部编码的行内编辑表单（action 缺失）")
	}
	if !strings.Contains(body, "name=\"externalSku\"") || !strings.Contains(body, "value=\"EXT-EDIT-1\"") {
		t.Fatalf("库存页应把新外码回显到行内输入框里")
	}
	// 表单必须带定位三元组（工程 / 仓库 / 变体），否则提交回不到那一行。
	for _, want := range []string{
		"name=\"variantId\" value=\"" + v.ID + "\"",
		"name=\"warehouseId\" value=\"" + wh.ID + "\"",
		"name=\"csrf_token\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("行内编辑表单缺少 %s", want)
		}
	}

	// done=1 的完成提示（本页既有 ok 之外新增的 done 口径，两种都要能显示）。
	body = externalSKUPageBody(t, engine, f.projectID, bare, "done=1")
	if !strings.Contains(body, "上一次操作已完成") {
		t.Fatalf("done=1 应在页面上给出完成提示")
	}
}

// TestExternalSKUEditRejectsInvalidCode 判据 2：非法外码（超长 / 控制字符）报中文且真源未写。
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
			loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, tc.code))
			if strings.Contains(loc, "done=1") {
				t.Fatalf("非法外码不该回带 done=1，实际 %q", loc)
			}
			errText := errTextOf(t, loc)
			if !strings.Contains(errText, "外部编码不合法") {
				t.Fatalf("错误文案应是中文的业务提示，实际 %q", errText)
			}
			if strings.Contains(errText, "ErrExternalSKUInvalid") {
				t.Fatalf("页面上不该出现裸 i18n key，实际 %q", errText)
			}
			// 回列表后模板把 err 回显出来（用户看得到那句中文）。
			body := externalSKUPageBody(t, engine, f.projectID, v.SKUCode, "err="+url.QueryEscape(errText))
			if !strings.Contains(body, "外部编码不合法") {
				t.Fatalf("库存页应回显中文错误文案")
			}
			// 真源一字未写。
			if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
				t.Fatalf("非法外码不该落库，实际 %q", got)
			}
		})
	}
}

// TestExternalSKUEditCrossProductConflict 判据 3：跨商品共用一个外码报中文且真源未写。
func TestExternalSKUEditCrossProductConflict(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, va := seedExternalSKUEditRow(t, f)
	// 另一个商品的变体（商品模块在它建仓时自动生成库存行）。
	pb := mustProductPriced(t, f, "冲突商品", 19.9)
	vb := f.firstVariant(t, pb.ID)

	// 商品 A 先占住 EXT-SHARED（同一商品多口味共用是合法形态，这里只占一个变体）。
	if loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, va.ID, va.SKUCode, "EXT-SHARED")); !strings.Contains(loc, "done=1") {
		t.Fatalf("商品 A 登记外码应成功，实际 %q", loc)
	}
	// 商品 B 用同一个外码 → 必须被拒（N:1 弱校验）。
	loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, vb.ID, vb.SKUCode, "EXT-SHARED"))
	if strings.Contains(loc, "done=1") {
		t.Fatalf("跨商品共用外码不该成功，实际 %q", loc)
	}
	errText := errTextOf(t, loc)
	if !strings.Contains(errText, "该外部编码在本仓已挂在另一个商品上") {
		t.Fatalf("冲突错误应是中文的业务提示，实际 %q", errText)
	}
	if strings.Contains(errText, "ErrExternalSKUProductConflict") {
		t.Fatalf("页面上不该出现裸 i18n key，实际 %q", errText)
	}
	// 被拒后真源未写：B 仍为空，A 保持不变。
	if got := externalSKUOf(t, f, vb.ID, wh.ID); got != "" {
		t.Fatalf("冲突时真源不该被写，实际 %q", got)
	}
	if got := externalSKUOf(t, f, va.ID, wh.ID); got != "EXT-SHARED" {
		t.Fatalf("冲突不该影响已占位那一行，实际 %q", got)
	}
}

// TestExternalSKUEditClearOnlyTouchesExternalCode 判据 4/5：清空成功，且 sku_code / 数量 / 成本逐项不变。
func TestExternalSKUEditClearOnlyTouchesExternalCode(t *testing.T) {
	engine, f := newExternalSKUEditEngine(t)
	if engine == nil {
		return
	}
	wh, _, v := seedExternalSKUEditRow(t, f)

	// 先登记一个外码 —— 否则「清空」测的是一个空转。
	if loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-CLEAR")); !strings.Contains(loc, "done=1") {
		t.Fatalf("登记外码应成功，实际 %q", loc)
	}
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

	// 清空：空串是**合法值**（撤销映射），必须是成功而不是「保存失败」。
	if loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "")); !strings.Contains(loc, "done=1") {
		t.Fatalf("清空外码应成功并带回 done=1，实际 %q", loc)
	}
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

	// 页面不再回显旧外码（空串渲染成空输入框，不再是那一行的旧值）。
	body := externalSKUPageBody(t, engine, f.projectID, v.SKUCode, "")
	if strings.Contains(body, "value=\"EXT-CLEAR\"") {
		t.Fatalf("清空后页面不该再回显旧外码")
	}
	if !strings.Contains(body, "name=\"externalSku\"") {
		t.Fatalf("清空后行内编辑输入框应仍在（空 = 该仓用我们自己的 SKU）")
	}

	// 空白串与空串同义（归一在 service）：再登记一次，用纯空白清空。
	if loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-AGAIN")); !strings.Contains(loc, "done=1") {
		t.Fatalf("再次登记外码应成功，实际 %q", loc)
	}
	if loc := externalSKURedirect(t, postExternalSKU(engine, f.projectID, wh.ID, v.ID, v.SKUCode, "   ")); !strings.Contains(loc, "done=1") {
		t.Fatalf("纯空白应等同于清空，实际 %q", loc)
	}
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
		t.Fatalf("纯空白提交后真源外码应为空串，实际 %q", got)
	}
}

// TestExternalSKUEditUnauthorizedBlocked 判据 6：未授权路径仍按**既有权限点**拦截。
//
// 生产把这条页面路由挂在 builtin.CasbinMiddlewareForPath("/api/inventory/stock/change")
// 上（见 inventory_router.go；页面组不经过 authorizedAPI 的 Casbin 中间件，权限只来自这一行），
// 权限点 inventory:stock_change 是库存页既有写入口复用的那一个，本批不新增权限点。
// 这里用同一条中间件 + 同一个权限点复现三种情况：
//
//	① 没有 user_id（未登录）→ 401；
//	② 有 user_id 但没有该权限点的策略 → 403，且真源一字未写；
//	③ 补上该权限点的策略后放行 → 证明拒绝确实由这个权限点决定，而不是「谁都不许写」。
//
// 最后再核对生产装配里那一行的权限点没被改掉：页面路由不经过 Casbin 组，
// 漏挂 / 改挂这一行不会让任何编译或既有测试变红，只会变成「登录即可写」。
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
		if userID != nil {
			uid := userID
			e.Use(func(c *gin.Context) { c.Set("user_id", uid) })
		}
		e.POST("/admin/inventory/external-sku",
			builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), handle.InventoryExternalSKUUpdate)
		return e
	}

	// ① 未登录（连 user_id 都没有）。
	rec := postExternalSKU(newEngine(nil), f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-DENIED-1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录提交应 401，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// ② 已登录但未授予 inventory:stock_change。
	const deniedUser int64 = 4242
	rec = postExternalSKU(newEngine(deniedUser), f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-DENIED-2")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限提交应 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "" {
		t.Fatalf("被拦下的请求不该改动真源，实际外码 %q", got)
	}

	// ③ 授予该权限点后同一条路径放行。
	enforcer := pkgcasbin.GetEnforcer()
	if enforcer == nil {
		t.Fatal("Casbin enforcer 未就绪")
	}
	sub := strconv.FormatInt(deniedUser, 10)
	if _, err := enforcer.AddPolicy(sub, "/api/inventory/stock/change", "POST", "inventory:stock_change"); err != nil {
		t.Fatalf("授予 inventory:stock_change 失败: %v", err)
	}
	rec = postExternalSKU(newEngine(deniedUser), f.projectID, wh.ID, v.ID, v.SKUCode, "EXT-ALLOWED")
	if rec.Code != http.StatusFound {
		t.Fatalf("授予权限点后应放行（302 回列表），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := externalSKUOf(t, f, v.ID, wh.ID); got != "EXT-ALLOWED" {
		t.Fatalf("放行后真源外码应为 EXT-ALLOWED，实际 %q", got)
	}

	// ④ 生产装配里这条路由挂的中间件与权限点，必须就是上面验证的那一个。
	src, err := os.ReadFile("../../../../internal/module/inventory/inbound/http/inventory_router.go")
	if err != nil {
		t.Fatalf("读库存路由装配失败: %v", err)
	}
	want := `pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change")`
	if !strings.Contains(string(src), want) {
		t.Fatalf("页面路由 /admin/inventory/external-sku 没有按既有权限点 inventory:stock_change 挂中间件")
	}
}

// TestExternalSKUEditI18nSeeded 迁移 255 的词条真的落库了（中英各一行）。
//
// 为什么单独测它：页面上那条中文提示在线上来自 sys_i18n 取词，模板兜底只在**取不到词条**时生效 ——
// 只测页面文字的话，缺 seed 也会被兜底掩盖过去（中文站点看起来完全正常，英文站点退回中文）。
// 词条只可能来自迁移 255 的 seed，所以这个断言同时是「255 已注册且执行」的证据。
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
	// 词条值必须是真文案（等于 key 就是「没 seed」的形态）。
	var value string
	if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'",
		"admin.inventory.sku.externalSku.save").Scan(&value).Error; err != nil {
		t.Fatalf("读中文词条失败: %v", err)
	}
	if strings.TrimSpace(value) == "" || strings.Contains(value, "admin.inventory") {
		t.Fatalf("中文词条应是一条真文案，实际 %q", value)
	}
}
