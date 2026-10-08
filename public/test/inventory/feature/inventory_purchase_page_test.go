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
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/shell"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	productdto "go_wp/internal/module/product/dto"

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
	// 新建采购单进右侧抽屉后，表单与写入口都按权限渲染（shell.Prepare 读 PermSetKey）；
	// 这条链路不走鉴权中间件，注入一份权限，让断言「页面里存在原生表单写入口」保持有效。
	// 生产入库表单也在本页（本批补回），所以权限集合要一并注入：页面按 PermSetKey 决定
	// 表单渲染不渲染，漏注入的表现是「表单没渲染」而不是模板报错。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"inventory:purchase_create":     true,
			"inventory:purchase_production": true,
		})
		c.Set(shell.ButtonsKey, map[string]bool{
			"inventory.purchase_create":     true,
			"inventory.purchase_production": true,
		})

	})
	handle := inventoryhttp.NewInventoryPurchasePageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory/purchases", handle.InventoryPurchasesPage)
	engine.POST("/admin/inventory/purchases/create", handle.InventoryPurchaseCreate)
	engine.POST("/admin/inventory/purchases/receipt", handle.InventoryPurchaseReceipt)
	engine.POST("/admin/inventory/purchases/production", handle.InventoryPurchaseProduction)
	// 生产入库（自家工厂）的表单**在本页**（本批补回：路由与权限点一直在，缺的是页面入口）；
	// 进货历史已并入库存流水的 SKU / 原因筛选，这里同步注册库存页路由供下面那段断言使用。
	pageHandle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", pageHandle.InventoryPage)
	return engine, f
}

// purchaseLineSkuField 新建采购单里「一行 SKU」的字段名（"<变体ID>|<商品ID>|<仓库侧裸码>"）。
//
// 这里独立写一份字面量而**不**引用 inventoryhttp 的常量：测试要断言的正是「页面渲染出来的
// 字段名」，从被测包取常量就变成了自证 —— 改了字段名而漏改模板时测试照样绿。
const purchaseLineSkuField = "lineSku"

// renderedLineSkuOption 从渲染出的采购页 HTML 里取出某变体的 SKU 选项值
// （"<变体ID>|<商品ID>|<仓库侧裸码>"）。
//
// 刻意**解析渲染结果**而不是在测试里拼一份期望值：模板与 handler 的字段名 / 值形状
// 对不上时（本批修掉的正是这类缺陷），手写的测试会照样通过 —— 只有从页面里取才钉得住。
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
	// 仓库侧（库存行 / 流水 / 采购行）一律用**裸码**：前缀只留在商品 / 变体侧。
	bare := bareSKU(v.SKUCode, wh.Code)

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	// 空白态：建单抽屉与它的写入口都在（收货表单要等有采购单才会出现）；
	// 生产入库表单同样在（它不依赖采购单），完整的「解析 → 提交 → 落账」断言见
	// inventory_production_form_test.go。
	for _, want := range []string{
		"采购入库", "新建采购单",
		"name=\"csrf_token\"",
		"action=\"/admin/inventory/purchases/create?",
		"action=\"/admin/inventory/purchases/production?",
		"data-drawer-open=\"#tpl-purchase-create\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("采购入库页缺少 %s", want)
		}
	}

	// 库存管理页已退化为只读视图 + 盘点 / 报损调整入口：生产入库的表单不再挂在它上面，
	// 但页面必须给出**指向正确位置的提示**（「入库 / 出库请走单据」），否则用户会照旧找
	// 那个已经不存在的按钮。这里断言那条边界提示与它的去处。
	invBody := httptestGet(engine, "/admin/inventory?project="+f.projectID).Body.String()
	for _, want := range []string{
		"生产入库", "/admin/inventory/purchases",
	} {
		if !strings.Contains(invBody, want) {
			t.Fatalf("库存页缺少「入库走单据」的边界提示 %s", want)
		}
	}
	if strings.Contains(invBody, "action=\"/admin/inventory/production\"") {
		t.Fatalf("库存管理页不应再有内联的生产入库表单")
	}

	// 每行 SKU 的**字段名**必须由模板渲染出来，且与 handler 读的是同一个名字。
	// 这条断言钉的是原缺陷：handler 读 lineSKUCode、模板里根本没有这个字段 →
	// 空串静默通过，采购行的仓库侧编码一直是空的（入库建库存行时才会炸）。
	if !strings.Contains(body, "name=\""+purchaseLineSkuField+"\"") {
		t.Fatalf("新建采购单表单必须渲染出 handler 读取的 SKU 字段 %q", purchaseLineSkuField)
	}
	// 选项值就是**页面真实渲染的东西**：从渲染结果里取，而不是在测试里手写一份
	// 「我以为的字段名 + 值形状」—— 两边对不上时手写的测试照样会通过（这正是原来的 bug）。
	lineSku := renderedLineSkuOption(t, body, v.ID)
	if !strings.HasSuffix(lineSku, "|"+bare) {
		t.Fatalf("选项值应带上仓库侧裸码 %q，实际 %q", bare, lineSku)
	}

	// 原生表单建采购单（一行）。
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
	// 成功：提示页（取代原先的 302 + ?ok=）。
	assertInventoryJump(t, rec, "ok")
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
	// 表单送上来的三个快照列都要落对：变体 / 商品 / **仓库侧裸码**（后者是本次的核心）。
	if order.Lines[0].VariantID != v.ID || order.Lines[0].ProductID != p.ID || order.Lines[0].SKUCode != bare {
		t.Fatalf("采购行快照不正确（SKU 必须是仓库侧裸码 %q）：%+v", bare, order.Lines[0])
	}
	// 表单没提交该字段时（例如有人只改了模板、或外部直接构造请求）不再是「空串静默通过」：
	// 服务端当场拒绝，并把可行动的原因渲染成失败提示页。
	rec = postForm(engine, "/admin/inventory/purchases/create", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "code": {"po-page-empty"},
		"sourceId": {ext.ID}, "warehouseId": {wh.ID},
		"lineQuantity": {"1"}, "lineUnitPrice": {"1"},
	})
	assertInventoryJump(t, rec, "err", "采购单至少要有一行")
	if got, lerr := f.inventory.ListPurchaseOrders(ctx, &inventorydto.ListPurchaseOrderReq{ProjectID: f.projectID}); lerr != nil || len(got) != 1 {
		t.Fatalf("被拒绝的建单不应留下采购单：%v %+v", lerr, got)
	}

	// 有单据后重新渲染：每一行都带「登记入库」原生表单（幂等键来自页面的隐藏域）。
	rec = httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "action=\"/admin/inventory/purchases/receipt?") {
		t.Fatalf("有采购单后页面应渲染收货表单，实际 %d", rec.Code)
	}

	// 收货表单（幂等键来自页面的隐藏域）。
	receiptForm := url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"4"}, "requestId": {"PAGE-RECV-1"},
	}
	rec = postForm(engine, "/admin/inventory/purchases/receipt", receiptForm)
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("表单收货后真源应为 4，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusReceived {
		t.Fatalf("收满后状态应为 received，实际 %q", got)
	}
	// 同一份表单被重复提交（浏览器双击 / 回退重发）：幂等键挡住，库存只加一次。
	rec = postForm(engine, "/admin/inventory/purchases/receipt", receiptForm)
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("重复提交不应二次加库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("重复提交不应产生第二张入库单，实际 %d", got)
	}

	// 生产入库表单：无采购单 + 成本价手工填写。字段与值全部**从渲染出的 HTML 解析**
	//（不手写字段名）：字段名对不上正是原始缺陷的根因，手写字段名的测试钉不住它。
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

	// 进货历史按 SKU 过滤：采购收货与生产入库都在同一张历史里。
	// 这张历史已随改造并入库存流水（按 SKU / 原因筛选），采购页不再挂它 —— 断言换目标页面到
	// /admin/inventory（引擎上方已注册）。采购收货与生产入库是两条原因不同的流水，
	// 用流水行里的原因 code 快照来断言（页头按钮也写着「生产入库」，只断言那四个字会失真）。
	// 库存页的 SKU 筛选是仓库侧维度：流水与库存行存的都是裸码。
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

	// 业务错误渲染失败提示页（这里已收满的单再收一次）：文案是中文、不是裸 key。
	rec = postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"1"}, "requestId": {"PAGE-RECV-2"},
	})
	assertInventoryJump(t, rec, "err", "采购单已全部入库，无需再收")
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
	if !strings.Contains(body, "stack purchase-page") {
		t.Fatalf("页面缺少多端适配的根类 purchase-page")
	}
	// 键盘可滚的表格容器（宽表在窄视口下靠它横向滚动，且能 Tab 聚焦后用方向键滚）。
	// 容器类由 pages-table-wrap 迁到公共类 table-wrap（滚动与键盘聚焦都在 .table-wrap 上）；
	// 采购单表同时带 .table-scroll（宽表横向滚动），两者与 tabindex 同在一个容器上。
	if !strings.Contains(body, `class="table-wrap table-scroll" tabindex="0"`) {
		t.Fatalf("采购单表应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}
	// 窄屏堆叠态靠 data-label 回显列名（列名不随表头消失而丢失）。
	// 列集合随改造换了：单头进表格（采购单号 / 货源 / 收货仓 / 单据状态 / 收货进度 / 下单时间 /
	// 操作），行明细与「登记入库 / 单价」在抽屉里，所以这里断言表格真实的列。
	if !strings.Contains(body, "data-label=\"采购单号\"") || !strings.Contains(body, "data-label=\"操作\"") {
		t.Fatalf("表格单元应带 data-label（窄屏堆叠时回显列名）")
	}
	// 模板不含任何 <script>：交互全由原生表单提交完成（layout 里的全局壳不算本页引入）。
	tplRaw, terr := os.ReadFile(inventoryAdminTemplatePath("inventory_purchases.html"))
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
	// 本页三个写入口 —— 建单 / 逐行收货 / 生产入库，各一处；GET 的筛选与工程切换表单不带。
	// 建单表单已抽出为 inventory_purchase_create_form.html（页面与失败回显片段共用同一真源），
	// 所以计数覆盖页面模板 + 新建片段两个文件，契约本身不变。
	fragRaw, ferr := os.ReadFile(inventoryAdminTemplatePath("inventory_purchase_create_form.html"))
	if ferr != nil {
		t.Fatalf("读采购新建片段模板失败: %v", ferr)
	}
	if got := strings.Count(string(tplRaw)+string(fragRaw), "name=\"csrf_token\""); got != 3 {
		t.Fatalf("建单 / 收货 / 生产入库三个写表单各要带一处 csrf_token 隐藏域，页面+片段里共有 %d 处", got)
	}
	if got := strings.Count(string(tplRaw), "method=\"get\""); got != 2 {
		t.Fatalf("工程切换 + 采购单筛选都用 GET，模板里有 %d 处", got)
	}

	// 样式表契约：断点内的堆叠块 + 宽度 min(100%, …) 收口 + 输入不写死像素。
	raw, err := os.ReadFile(templateRoot() + "/static/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)
	// 断言的是**真实在用**的规则：`.receipt-form` 已从模板与样式表一并清掉（该 class 无任何
	// 使用点，css_class_audit_test 把它登记为疑似死样式）—— 继续要求它存在，等于把「清理死样式」
	// 变成红灯。要恢复这条断言，先在模板里真的用上这个 class。
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

func inventoryAdminTemplatePath(name string) string {
	return filepath.Join(templateRoot(), "admin", "inventory", name)
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
