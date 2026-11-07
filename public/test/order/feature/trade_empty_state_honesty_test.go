package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/adminctx"
	orderenums "go_wp/internal/module/order/enums"
	orderhttp "go_wp/internal/module/order/inbound/http"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

type tradePageEnv struct {
	engine  *gin.Engine
	db      *gorm.DB
	project string
}

func newTradePageEnv(t *testing.T) *tradePageEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "交易域空态测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	orders := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db),
		ordermodel.NewReturnModel(db),
		nil, nil, nil, nil,
	)
	orders.SetProjects(projects)

	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"order:create": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"order.create": true})
		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal/templates"), true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们失败会落成空响应。
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	pages := orderhttp.NewOrderPageHandle(orders, projects, nil)
	returns := orderhttp.NewReturnPageHandle(orders, projects, nil, nil)
	engine.GET("/admin/orders", pageview.Slot(orderhttp.OrderListPageTemplate, pageview.RenderPage), pages.OrdersPage)
	engine.GET("/admin/returns", pageview.Slot(orderhttp.ReturnListPageTemplate, pageview.RenderPage), returns.ReturnsPage)

	return &tradePageEnv{engine: engine, db: db, project: project.ID}
}

func (e *tradePageEnv) get(t *testing.T, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 状态码 %d", path, rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("GET %s 渲染中断（缺 </html>）—— 模板里有缺键的 if", path)
	}
	return body
}

func emptyActionHrefs(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, `class="empty-actions"`)
		if i < 0 {
			return out
		}
		rest = rest[i:]
		end := strings.Index(rest, "</div>")
		if end < 0 {
			end = len(rest)
		}
		out = append(out, hrefsIn(rest[:end])...)
		rest = rest[end:]
	}
}

func pageActionHrefs(body string) []string {
	i := strings.Index(body, `class="page-actions"`)
	if i < 0 {
		return nil
	}
	rest := body[i:]
	end := strings.Index(rest, "</header>")
	if end < 0 {
		end = len(rest)
	}
	return hrefsIn(rest[:end])
}

func hrefsIn(block string) []string {
	var out []string
	for {
		j := strings.Index(block, `href="`)
		if j < 0 {
			return out
		}
		block = block[j+len(`href="`):]
		k := strings.Index(block, `"`)
		if k < 0 {
			return out
		}
		out = append(out, block[:k])
		block = block[k+1:]
	}
}

func containsHref(hrefs []string, path string) bool {
	for _, h := range hrefs {
		if strings.HasPrefix(h, path) {
			return true
		}
	}
	return false
}

func emptyDescText(body string) string {
	i := strings.Index(body, `class="empty-desc"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	end := strings.Index(rest, "</p>")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

func TestOrdersEmptyStateActionsFollowFilter(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}

	bare := env.get(t, "/admin/orders?project="+env.project)
	bareHrefs := emptyActionHrefs(bare)
	if len(bareHrefs) != 1 {
		t.Fatalf("无筛选时应有且只有建单入口一个空态主行动，实际 %v", bareHrefs)
	}
	if !containsHref(bareHrefs, "/admin/orders/new") {
		t.Errorf("无筛选时的空态主行动必须是建单入口，实际 %v", bareHrefs)
	}
	if containsHref(bareHrefs, "/admin/orders?") {
		t.Errorf("无筛选时不该给「重置」（指向本页自己的死按钮），实际 %v", bareHrefs)
	}

	filtered := env.get(t, "/admin/orders?project="+env.project+"&keyword=ZZPROBEXYZZ")
	hrefs := emptyActionHrefs(filtered)
	if len(hrefs) != 2 {
		t.Fatalf("带筛选时应有两个空态主行动（建单 + 重置），实际 %v", hrefs)
	}
	if !containsHref(hrefs, "/admin/orders/new") {
		t.Errorf("带筛选时的空态也要给建单入口，实际 %v", hrefs)
	}
	reset := hrefs[1]
	if !strings.Contains(reset, "project="+env.project) {
		t.Errorf("「重置」必须带上当前工程，否则多工程下会掉回默认工程：%s", reset)
	}
	if strings.Contains(reset, "keyword=") {
		t.Errorf("「重置」不能把筛选条件带回去（那等于没重置）：%s", reset)
	}
}

func TestOrdersEmptyCopyMatchesRoutes(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}

	snapshot, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "routers", "testdata", "routes.snapshot"))
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}

	hasCreateRoute := false
	for _, line := range strings.Split(string(snapshot), "\n") {
		if strings.TrimSpace(line) == "GET /admin/orders/new" {
			hasCreateRoute = true
		}
	}
	if !hasCreateRoute {
		t.Fatalf("路由表里没有 GET /admin/orders/new —— 建单页的实现被回退了（本判据的前提已不成立）")
	}
	body := env.get(t, "/admin/orders?project="+env.project)
	if !containsHref(emptyActionHrefs(body), "/admin/orders/new") {
		t.Errorf("路由表里有建单页，但空态没有给出入口（文案与路由表不一致）")
	}
	if !containsHref(pageActionHrefs(body), "/admin/orders/new") {
		t.Errorf("路由表里有建单页，但页头没有给出入口（双入口缺一）")
	}

	if desc := emptyDescText(body); strings.Contains(desc, "代客建单") {
		t.Errorf("空态描述里仍在描述「代客建单」：%q", desc)
	}
	if !strings.Contains(body, "台前下单后就会出现在这里") {
		t.Error("订单空态应说明「台前下单后就会出现在这里」")
	}

	assertI18nValueNotContains(t, env.db, "admin.orders.list.empty_tail", "zh-CN", "代客建单")
	assertI18nValueNotContains(t, env.db, "admin.orders.list.empty_tail", "en-US", "on their behalf")
}

func TestReturnsMissingDetailSaysNotFound(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrReturnNotFound: {"zh-CN": "退货申请不存在", "en-US": "Return request not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/returns?project="+env.project+"&returnId=999999", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("缺详情时应降级渲染（200 + 提示条），实际 %d —— "+
			"returnId 指向不存在的单把整页打成 500 是生产缺陷（控制器/模板侧），不在本测试修", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("缺详情时渲染中断（缺 </html>）")
	}
	alert := orderPageAlert(body)
	if !strings.Contains(alert, "退货申请不存在") {
		t.Errorf("缺详情时的提示应是「退货申请不存在」，实际 %q", alert)
	}
	if strings.Contains(alert, "系统内部错误") {
		t.Errorf("缺详情被归口成了系统故障：%q", alert)
	}

	invalid := orderPageAlert(env.get(t, "/admin/returns?project="+env.project+"&returnId=abc"))
	if strings.Contains(invalid, "退货申请不存在") {
		t.Errorf("非数字 id 应说「编号不合法」，实际 %q", invalid)
	}
}

func TestTradePagesI18nValuesAreActuallyUpdated(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}

	assertI18nValueEquals(t, db, "admin.customers.list.empty_heading", "zh-CN", "还没有客户")
	assertI18nValueContains(t, db, "admin.customers.list.empty_desc", "zh-CN", "后台不能直接新建")

	assertI18nValueEquals(t, db, "admin.customers.field.registered_at", "zh-CN", "注册时间（起）")
	assertI18nValueEquals(t, db, "admin.customers.field.registered_to", "zh-CN", "注册时间（止）")

	assertI18nValueEquals(t, db, "admin.customers.action.disable", "zh-CN", "停用")
	assertI18nValueEquals(t, db, "admin.customers.action.enable", "en-US", "Enable")

	for _, key := range []string{
		"admin.returns.note.requested", "admin.returns.note.approved", "admin.returns.note.received",
		"admin.returns.note.completed", "admin.returns.note.rejected", "admin.returns.note.cancelled",
	} {
		for _, lang := range []string{"zh-CN", "en-US"} {
			if got := i18nValue(db, key, lang); got == "" {
				t.Errorf("词条 %s（%s）缺失 —— 英文 / 中文界面会回落模板兜底", key, lang)
			}
		}
	}

	assertI18nValueEquals(t, db, orderenums.ErrReturnNotFound, "zh-CN", "退货申请不存在")

	if got := i18nValue(db, "admin.customers.empty", "zh-CN"); got != "" {
		t.Errorf("孤儿词条 admin.customers.empty 仍在库里（zh-CN=%q）—— 删能力要连 seed 与幂等条件一起收口", got)
	}
	if got := i18nValue(db, "admin.customers.empty", "en-US"); got != "" {
		t.Errorf("孤儿词条 admin.customers.empty 仍在库里（en-US=%q）", got)
	}
}

func TestReturnsStatusNoteMovedIntoHelp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "templates", "admin", "order", "returns.html"))
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	html := string(src)

	for _, key := range []string{
		"admin.returns.note.requested", "admin.returns.note.approved", "admin.returns.note.received",
		"admin.returns.note.completed", "admin.returns.note.rejected", "admin.returns.note.cancelled",
	} {
		if !strings.Contains(html, key) {
			t.Errorf("模板缺少状态说明取词 %s —— 该状态的说明会整块消失", key)
		}
	}

	if strings.Contains(html, "NoteKey") {
		t.Error("模板仍在取 NoteKey —— 状态说明没有改成模板内联取词")
	}
	if strings.Contains(html, "detail.Note") {
		t.Error("模板仍在正文里渲染 detail.Note —— 状态说明应整块收进 .help 悬浮")
	}

	i := strings.Index(html, "admin.returns.actions.heading")
	if i < 0 {
		t.Fatal("模板里找不到「操作」标题的取词 admin.returns.actions.heading")
	}
	rest := html[i:]
	j := strings.Index(rest, `class="help-pop" role="tooltip"`)
	k := strings.Index(rest, "admin.returns.note.requested")
	if j < 0 || k < 0 || k < j {
		t.Error("「操作」标题的 .help-pop 里没有按状态取词的说明（admin.returns.note.requested）—— 说明没有收进悬浮")
	}
}

func orderPageVisibleText(body string) string {
	const open = `<form class="lang-switch"`
	i := strings.Index(body, open)
	if i < 0 {
		return body
	}
	j := strings.Index(body[i:], "</form>")
	if j < 0 {
		return body
	}
	return body[:i] + body[i+j+len("</form>"):]
}

func TestOrdersPageErrParamIsIgnored(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrOrderNotFound: {"zh-CN": "订单不存在", "en-US": "Order not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, forged := range []string{
		orderenums.ErrOrderNotFound,
		url.QueryEscape("订单不存在"),
		url.QueryEscape(`relation "orders"`),
	} {
		body := env.get(t, "/admin/orders?project="+env.project+"&err="+forged)
		visible := orderPageVisibleText(body)

		if strings.Contains(visible, "订单不存在") {
			t.Errorf("?err=%s 的文案出现在了可见正文里 —— 页面不应再读取该参数", forged)
		}
		if strings.Contains(visible, orderenums.ErrOrderNotFound) {
			t.Errorf("?err=%s 的裸 key 出现在了可见正文里", forged)
		}
		if strings.Contains(visible, `relation "orders"`) {
			t.Errorf("?err=%s 的内部细节出现在了可见正文里", forged)
		}

		if !strings.Contains(body, "</html>") {
			t.Errorf("?err=%s 让渲染中断了（缺 </html>）", forged)
		}
		if orderPageAlert(body) != "" {
			t.Errorf("?err=%s 不应产生任何提示条，got %q", forged, orderPageAlert(body))
		}
	}
}

func TestOrdersMissingDetailSaysNotFound(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrOrderNotFound: {"zh-CN": "订单不存在", "en-US": "Order not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	alert := orderPageAlert(env.get(t, "/admin/orders?project="+env.project+"&orderId=999999"))
	if !strings.Contains(alert, "订单不存在") {
		t.Errorf("orderId 指向不存在的单时应说明原因，实际 %q", alert)
	}
	if strings.Contains(alert, "系统内部错误") {
		t.Errorf("缺详情被归口成了系统故障：%q", alert)
	}
}

type orderCreatePageEnv struct {
	engine  *gin.Engine
	fixture *orderFixture
	project string
}

func newOrderCreatePageEnv(t *testing.T) *orderCreatePageEnv {
	t.Helper()
	f := newOrderFixture(t)
	if f == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"order:create": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"order.create": true})
		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal/templates"), true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们失败会落成空响应。
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	dict := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(f.db), nil)
	page := orderhttp.NewOrderCreatePageHandle(f.orders, projects, f.products, dict)
	engine.GET("/admin/orders/new", pageview.Slot(orderhttp.OrderNewPageTemplate, pageview.RenderPage), page.OrderCreatePage)
	engine.POST("/admin/orders/create", pageview.HTMLFragment, page.OrderCreateSubmit)
	return &orderCreatePageEnv{engine: engine, fixture: f, project: f.projectID}
}

func (e *orderCreatePageEnv) get(t *testing.T, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 状态码 %d", path, rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("GET %s 渲染中断（缺 </html>）—— 模板里有缺键的 if", path)
	}
	return body
}

func (e *orderCreatePageEnv) post(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/orders/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func adminOrderCreateForm(project, variantID, email, requestID string) url.Values {
	return url.Values{
		"project":            {project},
		"requestId":          {requestID},
		"customerEmail":      {email},
		"customerName":       {"张三"},
		"customerPhone":      {"13800000000"},
		"paymentMethod":      {"paypal"},
		"paymentMethodTitle": {"PayPal（模拟）"},
		"shippingTotal":      {"1500"},
		"remark":             {"尽快发货"},
		"adminNote":          {"电话单，客户要求顺丰"},
		"candidateKeyword":   {"回填商品"},
		"shipName":           {"李四"},
		"shipPhone":          {"13900000000"},
		"shipProvince":       {"广东省"},
		"shipCity":           {"深圳市"},
		"shipDistrict":       {"南山区"},
		"shipAddress":        {"科技园 1 号"},
		"shipZip":            {"518000"},
		"billCity":           {"上海市"},
		"variantId":          {variantID},
		"quantity":           {"2"},
	}
}

func TestAdminOrderCreatePageKeepsFieldSemantics(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, variantID := env.fixture.addProduct(t, "回填商品", 12.5, 5)

	page := env.get(t, "/admin/orders/new?project="+env.project+"&keyword=回填商品&rows=3")
	for _, want := range []string{
		`hx-post="/admin/orders/create"`,
		`name="variantId"`,
		`name="quantity"`,
		`data-write-result`,
		`data-write-nav="[data-order-next]"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("建单页缺少 %q", want)
		}
	}
	if got := strings.Count(page, `name="quantity"`); got != 3 {
		t.Errorf("rows=3 时应渲染 3 行明细输入框，实际 %d", got)
	}
	if !strings.Contains(page, "回填商品") {
		t.Error("候选 SKU 应带出商品名（真商品目录的 ListVariantOptions）")
	}
	// 幂等键必须随表单提交：重试不会落两单。
	if !strings.Contains(page, `name="requestId"`) {
		t.Error("建单表单缺少幂等键 requestId")
	}

	// 失败提交只回错误片段，不替换已填内容。
	form := adminOrderCreateForm(env.project, variantID, "not-an-email", "req-echo-1")
	rec := env.post(t, form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("字段校验失败应回 400 错误片段，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", out)
	}
	if strings.Contains(out, "<html") {
		t.Fatalf("错误片段不该带整页壳：%s", out)
	}
	if strings.Contains(out, "order.err.") || strings.Contains(out, "order.msg.") {
		t.Fatalf("失败片段漏出了声明 key：%s", out)
	}
}

func TestAdminOrderCreateDefaultsToNoGuestAccount(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	f := env.fixture
	_, variantID := f.addProduct(t, "开号开关商品", 20, 5)

	rec := env.post(t, adminOrderCreateForm(env.project, variantID, "off@example.com", "req-off-1"))
	orderID := orderIDFromCreate(t, rec)
	if orderID == 0 {
		t.Fatal("成功跳转应带上新单的 orderId（回列表页时详情是展开的）")
	}

	var uid *uint64
	if err := f.db.Raw("SELECT user_id FROM orders WHERE id = ?", orderID).Scan(&uid).Error; err != nil {
		t.Fatalf("读订单 user_id 失败: %v", err)
	}
	if uid != nil {
		t.Errorf("默认（不勾开号）时 user_id 必须留空，实际 %d", *uid)
	}
	if m := f.mail.findTemplate("guest_account"); m != nil {
		t.Errorf("默认档不该发出初始密码邮件，实际收件人 %q", m.To)
	}
	var created int64
	if err := f.db.Raw("SELECT COUNT(*) FROM users WHERE lower(email) = 'off@example.com'").Scan(&created).Error; err != nil {
		t.Fatalf("查账号失败: %v", err)
	}
	if created != 0 {
		t.Errorf("默认档不该给客户建号，实际建了 %d 个", created)
	}

	optIn := adminOrderCreateForm(env.project, variantID, "on@example.com", "req-on-1")
	optIn.Set("provisionGuestAccount", "1")
	rec = env.post(t, optIn)
	orderID = orderIDFromCreate(t, rec)
	if err := f.db.Raw("SELECT user_id FROM orders WHERE id = ?", orderID).Scan(&uid).Error; err != nil {
		t.Fatalf("读订单 user_id 失败: %v", err)
	}
	if uid == nil {
		t.Error("显式勾选开号后订单必须关联账号（否则客户在账户中心看不到这单）")
	}
	if m := f.mail.findTemplate("guest_account"); m == nil || m.To != "on@example.com" {
		t.Errorf("显式勾选后应把初始密码寄给该邮箱，实际 %+v", m)
	}

	var head struct {
		Via    string
		Status string
	}
	if err := f.db.Raw("SELECT created_via AS via, status FROM orders WHERE id = ?", orderID).Scan(&head).Error; err != nil {
		t.Fatalf("读订单来源失败: %v", err)
	}
	if head.Via != "admin" {
		t.Errorf("后台建单的 created_via 应为 admin，实际 %q", head.Via)
	}
	if head.Status != "pending" {
		t.Errorf("建单后状态应为 pending，实际 %q", head.Status)
	}
}

func orderIDFromCreate(t *testing.T, rec *httptest.ResponseRecorder) uint64 {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("建单成功应回 200 + HTML 片段，实际 %d（%s）", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("建单成功应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-order-next`) {
		t.Fatalf("建单片段必须给出下一步链接（data-order-next）：%s", body)
	}
	// 下一步链接由 Jet 依据实体事实生成，前端按 data-write-nav 导航。
	loc := orderCreatedHref(t, body)
	if loc == "" {
		t.Fatalf("建单片段必须带下一步链接（回列表并展开新单），实际为空")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("Location 不是合法 URL：%q（%v）", loc, err)
	}
	if u.Path != "/admin/orders" {
		t.Fatalf("回跳应落在订单列表 /admin/orders，实际 %q", u.Path)
	}
	raw := u.Query().Get("orderId")
	if raw == "" {
		t.Fatalf("Location 必须带 orderId（回列表页时该单的详情是展开的）：%q", loc)
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		t.Fatalf("Location 里的 orderId 不是有效订单 id：%q（%v）", raw, err)
	}
	return id
}

// orderCreatedHref 从建单结论片段里取出下一步链接的 href。
func orderCreatedHref(t *testing.T, body string) string {
	t.Helper()
	marker := `data-order-next`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	hrefAt := strings.Index(rest, `href="`)
	if hrefAt < 0 {
		return ""
	}
	rest = rest[hrefAt+len(`href="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func i18nValue(db *gorm.DB, key, lang string) string {
	var value string
	if err := db.Raw(
		"SELECT COALESCE(item_value, '') FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang,
	).Scan(&value).Error; err != nil {
		return ""
	}
	return value
}

func assertI18nValueEquals(t *testing.T, db *gorm.DB, key, lang, want string) {
	t.Helper()
	if got := i18nValue(db, key, lang); got != want {
		t.Errorf("词条 %s（%s）= %q，期望 %q", key, lang, got, want)
	}
}

func assertI18nValueContains(t *testing.T, db *gorm.DB, key, lang, want string) {
	t.Helper()
	if got := i18nValue(db, key, lang); !strings.Contains(got, want) {
		t.Errorf("词条 %s（%s）= %q，应包含 %q", key, lang, got, want)
	}
}

func assertI18nValueNotContains(t *testing.T, db *gorm.DB, key, lang, unwanted string) {
	t.Helper()
	if got := i18nValue(db, key, lang); strings.Contains(got, unwanted) {
		t.Errorf("词条 %s（%s）= %q，不应包含 %q", key, lang, got, unwanted)
	}
}
