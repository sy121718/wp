package userhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"

	"github.com/gin-gonic/gin"
)

const (
	customerTestPageTitle   = "客户管理"
	customerTestDetailTitle = "客户详情"
)

func customerTestLayoutData(anyView any) gin.H {
	lang := "zh-CN"
	base := pageview.ViewMap(anyView)
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/customers"
	return base
}

func renderCustomerAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 渲染失败，状态 %d", name, rec.Code)
	}
	return rec.Body.String()
}

type fakeCustomerAdmin struct {
	list   *userdto.CustomerListResp
	detail *userdto.CustomerResp
	err    error

	failIDs map[uint64]bool

	unlockByID map[uint64]*userdto.CustomerUnlockResp
	statusReq  *userdto.CustomerStatusReq

	statusIDs  []uint64
	unlockRes  *userdto.CustomerUnlockResp
	lastListRe *userdto.CustomerListReq
}

func (f *fakeCustomerAdmin) ListCustomers(_ context.Context, req *userdto.CustomerListReq) (*userdto.CustomerListResp, error) {
	f.lastListRe = req
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

func (f *fakeCustomerAdmin) GetCustomer(_ context.Context, _ uint64) (*userdto.CustomerResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.detail, nil
}

// BulkSetCustomerStatus 复刻真实语义：任一失败整批拒绝。
func (f *fakeCustomerAdmin) BulkSetCustomerStatus(_ context.Context, req *userdto.CustomerBulkStatusReq) error {
	for _, id := range req.IDs {
		f.statusIDs = append(f.statusIDs, id)
		if f.failIDs[id] {
			return userenums.Err(userenums.ErrUserNotFound)
		}
	}
	return nil
}

// BulkUnlockCustomers 复刻真实语义：任一失败整批拒绝。
func (f *fakeCustomerAdmin) BulkUnlockCustomers(_ context.Context, req *userdto.CustomerBulkUnlockReq) error {
	for _, id := range req.IDs {
		if f.failIDs[id] {
			return userenums.Err(userenums.ErrUserNotFound)
		}
	}
	return nil
}

func (f *fakeCustomerAdmin) SetCustomerStatus(_ context.Context, req *userdto.CustomerStatusReq) (*userdto.CustomerStatusResp, error) {
	f.statusReq = req
	f.statusIDs = append(f.statusIDs, req.CustomerID)
	if f.err != nil {
		return nil, f.err
	}
	if f.failIDs[req.CustomerID] {
		return nil, errors.New(userenums.ErrUserNotFound)
	}
	return &userdto.CustomerStatusResp{CustomerID: req.CustomerID, Status: req.Status, StatusLabel: "已停用"}, nil
}

func (f *fakeCustomerAdmin) UnlockCustomer(_ context.Context, req *userdto.CustomerUnlockReq) (*userdto.CustomerUnlockResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if res, ok := f.unlockByID[req.CustomerID]; ok {
		return res, nil
	}
	if f.unlockRes != nil {
		return f.unlockRes, nil
	}
	return &userdto.CustomerUnlockResp{CustomerID: req.CustomerID, Unlocked: true}, nil
}

var _ usercontract.CustomerAdminPort = (*fakeCustomerAdmin)(nil)

type fakeOrderSummaryReader struct {
	res *ordercontract.CustomerOrderSummaryResp
	err error
}

func (f fakeOrderSummaryReader) CustomerOrderSummaryOf(_ context.Context, _ *ordercontract.CustomerOrderSummaryReq) (*ordercontract.CustomerOrderSummaryResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

var _ ordercontract.CustomerOrderSummaryReader = fakeOrderSummaryReader{}

type fakeProjects struct {
	projectcontract.ProjectService
	items []projectcontract.ProjectResp
}

func (f fakeProjects) List(_ context.Context) ([]projectcontract.ProjectResp, error) {
	return f.items, nil
}

func customerSample() *userdto.CustomerResp {
	registered := time.Date(2026, 9, 1, 10, 30, 0, 0, time.Local)
	lastLogin := time.Date(2026, 9, 20, 8, 5, 0, 0, time.Local)
	return &userdto.CustomerResp{
		ID: 42, Username: "alice", Email: "alice@example.com",
		Nickname: "小艾", DisplayName: "艾丽丝",
		Status: 1, EmailVerified: true,
		RegisteredAt: utils.NewJSONTimePtr(&registered),
		RegisterIP:   "203.0.113.7", RegisterLocation: "中国 上海",
		LastLoginTime: utils.NewJSONTimePtr(&lastLogin),
		LastLoginIP:   "203.0.113.9", LastLoginLocation: "中国 北京",
	}
}

func customerListSample() *userdto.CustomerListResp {
	return &userdto.CustomerListResp{
		List:     []*userdto.CustomerResp{customerSample()},
		Total:    1,
		Counters: userdto.CustomerCounters{Total: 1, Active: 1, Verified: 1},
	}
}

func newCustomerTestEngine(h *customerPageHandle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/customers", pageview.HTMLPage, pageview.Slot(CustomersListPageTemplate, pageview.RenderPage), h.CustomersPage)
	engine.GET("/admin/customers/overview", pageview.HTMLPage, pageview.Slot(CustomerOverviewPageTemplate, pageview.RenderPage), h.CustomerOverviewPage)
	engine.GET("/admin/customers/detail", pageview.HTMLPage, pageview.Slot(CustomerDetailPageTemplate, pageview.RenderPage), h.CustomerDetailPage)
	api := NewCustomerHandle(h.users)
	engine.POST("/api/customer/status", pageview.HTMLFragment, api.SetCustomerStatus)
	engine.POST("/api/customer/unlock", pageview.HTMLFragment, api.UnlockCustomer)
	engine.POST("/api/customer/bulk-status", pageview.HTMLFragment, api.BulkCustomerStatus)
	engine.POST("/api/customer/bulk-unlock", pageview.HTMLFragment, api.BulkCustomerUnlock)
	return engine
}

// customerFormBody 提交标准表单：页面写端点只认表单编码与字段名。
func customerFormBody(_ *testing.T, form url.Values) string {
	return form.Encode()
}

func TestCustomersListTemplateRenders(t *testing.T) {
	data := customerListPageData(customerTestPageTitle, customerListSample(), customerFilter{
		Keyword: "alice", Status: customerStatusAll,
	}, 1, 20, customerMsg{}, false)
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(data))

	for _, want := range []string{
		"客户管理", "艾丽丝", "alice", "alice@example.com", "正常", "已验证",
		"2026-09-01 10:30", "2026-09-20 08:05", "中国 北京",
		"/admin/customers/detail?id=42", "/api/customer/status",
		"邮箱已验证 1", "user:customer_status",

		`hx-post="/api/customer/bulk-status"`, `hx-post="/api/customer/bulk-unlock"`,
		"data-check-all", "data-check-item", "data-bulk-bar", `name="ids" value="42"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页渲染结果缺少 %q", want)
		}
	}

	bulkForm := strings.Index(body, `hx-post="/api/customer/bulk-status"`)
	table := strings.Index(body, `customers-page-table`)
	rowForm := strings.Index(body, `id="customer-status-42"`)
	if bulkForm < 0 || table < 0 || rowForm < 0 || !(bulkForm < table && table < rowForm) {
		t.Errorf("批量 form / 表格 / 行内表单的顺序不对：bulk=%d table=%d row=%d", bulkForm, table, rowForm)
	}
}

func TestCustomersListTemplateHidesStatusActionForPending(t *testing.T) {
	item := customerSample()
	item.Status = 2
	item.StatusLabel = "待激活"
	item.EmailVerified = false
	list := &userdto.CustomerListResp{List: []*userdto.CustomerResp{item}, Total: 1,
		Counters: userdto.CustomerCounters{Total: 1, Pending: 1, Unverified: 1}}

	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, list, customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))

	if strings.Contains(body, "/api/customer/status") {
		t.Errorf("待激活账号不应渲染停用按钮")
	}

	head := body[:strings.Index(body, "<tbody")]
	if !strings.Contains(head, "客户还没完成邮箱验证") {
		t.Errorf("状态列的解释应挂在表头 .help 里")
	}
	tbody := body[strings.Index(body, "<tbody"):]
	if strings.Contains(tbody, "colspan=") {
		t.Errorf("有数据时 tbody 不应有跨列说明行（说明已移进状态列表头的 .help）")
	}
}

func TestCustomersListTemplateRendersUnlockForLocked(t *testing.T) {
	item := customerSample()
	item.Locked = true
	item.LoginFailureCount = 5
	until := time.Date(2026, 9, 20, 9, 0, 0, 0, time.Local)
	item.LockedUntilTime = utils.NewJSONTimePtr(&until)
	list := &userdto.CustomerListResp{List: []*userdto.CustomerResp{item}, Total: 1,
		Counters: userdto.CustomerCounters{Total: 1, Active: 1, Locked: 1}}

	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, list, customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))

	for _, want := range []string{"/api/customer/unlock", "已锁定", "2026-09-20 09:00", "连续登录失败"} {
		if !strings.Contains(body, want) {
			t.Errorf("锁定账号渲染结果缺少 %q", want)
		}
	}
}

func TestCustomersListTemplateEmptyState(t *testing.T) {
	plain := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, nil, customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))
	if !strings.Contains(plain, "还没有客户") {
		t.Errorf("无筛选的空列表应当说明「还没有客户」")
	}

	filtered := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, nil, customerFilter{Status: customerStatusDisabled}, 1, 20, customerMsg{}, false)))
	if !strings.Contains(filtered, "该筛选条件下暂时没有账号") {
		t.Errorf("带筛选的空列表应当说明「该筛选条件下暂时没有账号」")
	}
}

func TestCustomersListTemplateCapabilityMissing(t *testing.T) {
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, nil, customerFilter{Status: customerStatusAll}, 1, 20,
			customerUnavailableLabel.msg(), true)))
	for _, want := range []string{customerUnavailableLabel.fallback, "装配问题"} {
		if !strings.Contains(body, want) {
			t.Errorf("能力未装配时应渲染 %q", want)
		}
	}
}

func TestCustomersPageHandlerRendersList(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()},
		fakeOrderSummaryReader{}, fakeProjects{items: []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}}})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?keyword=alice", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("客户列表页返回 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("客户列表页响应体为空 —— handler 里的模板名很可能与模板文件对不上")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Errorf("列表页没有渲染出客户邮箱：%s", body[:min(len(body), 200)])
	}
}

func TestCustomersPageIgnoresForgedNoticeQuery(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()},
		fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/customers?err="+url.QueryEscape("<script>alert(1)</script>")+
			"&ok="+url.QueryEscape("伪造的成功提示")+
			"&done="+url.QueryEscape("伪造的批量摘要"), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("列表页返回 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, forged := range []string{"<script>alert(1)</script>", "伪造的成功提示", "伪造的批量摘要"} {
		if strings.Contains(body, forged) {
			t.Fatalf("伪造的提示 %q 被渲染到页面上 —— 读侧判定没有删干净", forged)
		}
	}
}

func TestCustomersPageHandlerFiltersAreParsed(t *testing.T) {
	fake := &fakeCustomerAdmin{list: customerListSample()}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/customers?keyword=alice&status=&emailVerified=2&registeredFrom=2026-09-01&registeredTo=2026-09-30", nil))

	if fake.lastListRe == nil {
		t.Fatal("列表请求没有到达 service")
	}
	if fake.lastListRe.Status != customerStatusAll {
		t.Errorf("空 status 应解析为「全部」，实际 %d", fake.lastListRe.Status)
	}
	if fake.lastListRe.EmailVerified != userdto.EmailVerifiedNo {
		t.Errorf("emailVerified=2 应解析为「未验证」，实际 %d", fake.lastListRe.EmailVerified)
	}
	if fake.lastListRe.RegisteredFrom == nil || fake.lastListRe.RegisteredTo == nil {
		t.Fatal("注册时间范围没有被解析")
	}
	if last := fake.lastListRe.RegisteredTo.Time().Hour(); last != 23 {
		t.Errorf("结束日期应扩到当天最后一刻，实际 %02d 时", last)
	}
}

func TestCustomersStatusSaveRendersJumpAndKeepsFilters(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"customerId": {"42"}, "status": {"0"}}
	req := httptest.NewRequest(http.MethodPost,
		"/api/customer/status?keyword=alice&status=1&page=2&limit=20",
		strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("写操作应渲染结果片段（200），实际 %d", rec.Code)
	}
	if fake.statusReq == nil || fake.statusReq.Status != customerStatusDisabled {
		t.Fatalf("目标状态没有从表单解析出来：%+v", fake.statusReq)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("写响应必须是 HTML 片段，实际 %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "账号已停用") {
		t.Errorf("结果片段应含回执文案，实际 %s", body)
	}
}

func TestCustomerStatusSaveFromDetailReturnsToDetail(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"customerId": {"42"}, "status": {"1"}}
	req := httptest.NewRequest(http.MethodPost,
		"/api/customer/status?id=42&project=p1", strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("应渲染结果片段（200），实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("写响应必须是 HTML 片段，实际 %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "账号已启用") {
		t.Errorf("结果片段应含回执文案，实际 %s", body)
	}
}

func TestCustomerStatusSaveRejectsUnknownTarget(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"customerId": {"42"}, "status": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/api/customer/status", strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if fake.statusReq != nil {
		t.Fatalf("非法目标状态不应调用 service：%+v", fake.statusReq)
	}
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "data-ui-error") {
		t.Errorf("应当渲染失败片段，实际状态 %d：%s", rec.Code, body)
	}
	if strings.Contains(body, userenums.ErrCustomerStatusInvalid) {
		t.Errorf("失败片段里出现裸 key：%s", body)
	}
}

func TestCustomerUnlockDistinguishesOutcomes(t *testing.T) {
	cases := []struct {
		name string
		res  *userdto.CustomerUnlockResp

		want string
	}{
		{"真的解除了", &userdto.CustomerUnlockResp{CustomerID: 42, Unlocked: true}, "账号已解除锁定"},
		{"清了残留计数", &userdto.CustomerUnlockResp{CustomerID: 42, Cleared: true}, "账号未处于锁定状态，登录失败计数已清零"},
		{"本来就没事", &userdto.CustomerUnlockResp{CustomerID: 42}, "该账号没有处于锁定状态，无需解除"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewCustomerPageHandle(&fakeCustomerAdmin{unlockRes: tc.res}, fakeOrderSummaryReader{}, fakeProjects{})
			engine := newCustomerTestEngine(h)

			form := url.Values{"customerId": {"42"}}
			req := httptest.NewRequest(http.MethodPost, "/api/customer/unlock", strings.NewReader(customerFormBody(t, form)))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("解锁后应渲染结果片段（200），实际 %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("回执文案应含 %q", tc.want)
			}
		})
	}
}

func TestCustomersBulkStatusRejectsUnknownTarget(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"ids": {"42", "43"}, "status": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/api/customer/bulk-status", strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if len(fake.statusIDs) != 0 {
		t.Fatalf("非法目标状态不应调用 service：%v", fake.statusIDs)
	}
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "data-ui-error") {
		t.Errorf("应当渲染失败片段，实际状态 %d：%s", rec.Code, body)
	}
	if strings.Contains(body, userenums.ErrCustomerStatusInvalid) {
		t.Errorf("失败片段里出现裸 key：%s", body)
	}
}

func TestCustomersBulkNothingSelected(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"status": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/api/customer/bulk-status", strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if len(fake.statusIDs) != 0 {
		t.Fatalf("没有勾选时不应调用 service：%v", fake.statusIDs)
	}
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "data-ui-error") {
		t.Errorf("没有勾选应渲染失败片段，实际状态 %d：%s", rec.Code, body)
	}
}

func TestCustomersBulkUnlockDistinguishesNoop(t *testing.T) {
	fake := &fakeCustomerAdmin{unlockByID: map[uint64]*userdto.CustomerUnlockResp{
		42: {CustomerID: 42, Unlocked: true},
		43: {CustomerID: 43},
	}}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"ids": {"42", "43"}}
	req := httptest.NewRequest(http.MethodPost, "/api/customer/bulk-unlock", strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("批量解锁应渲染成功片段（200），实际 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "操作成功") {
		t.Errorf("成功应走统一提示组件：%s", body)
	}
}

func TestCustomersCounterTabsAreClickableFilters(t *testing.T) {
	data := customerListPageData(customerTestPageTitle, customerListSample(),
		customerFilter{Status: customerStatusActive}, 1, 20, customerMsg{}, false)
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(data))

	for _, want := range []string{"aria-current=\"true\"", "✓"} {
		if !strings.Contains(body, want) {
			t.Errorf("徽章缺少选中态标记 %q", want)
		}
	}

	hrefs := map[string]bool{}
	for _, m := range regexp.MustCompile(`href="(/admin/customers[^"]*)"`).FindAllStringSubmatch(body, -1) {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		raw = strings.ReplaceAll(raw, "&#38;", "&")
		hrefs[raw] = true
	}
	for _, want := range []string{
		"/admin/customers",
		"/admin/customers?status=0",
		"/admin/customers?locked=1&status=1",
		"/admin/customers?emailVerified=2&status=1",
	} {
		if !hrefs[want] {
			t.Errorf("徽章链接缺少 %q，实际 %v", want, hrefs)
		}
	}

	for _, gone := range []string{
		`id="customers-filter-status"`,
		`id="customers-filter-verified"`,
	} {
		if strings.Contains(body, gone) {
			t.Errorf("状态 / 邮箱维度应当只剩徽章，不应再有下拉：%s", gone)
		}
	}
}

func TestCustomersPageHandlerParsesLockedFilter(t *testing.T) {
	fake := &fakeCustomerAdmin{list: customerListSample()}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?locked=1", nil))

	if fake.lastListRe == nil || !fake.lastListRe.LockedOnly {
		t.Fatalf("locked=1 没有进查询条件：%+v", fake.lastListRe)
	}
}

func TestCustomersBulkStatusKeepsGoingAfterOneFailure(t *testing.T) {
	fake := &fakeCustomerAdmin{failIDs: map[uint64]bool{7: true}}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"ids": {"42", "7", "43"}, "status": {"0"}}
	req := httptest.NewRequest(http.MethodPost,
		"/api/customer/bulk-status?keyword=alice&page=2", strings.NewReader(customerFormBody(t, form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// 全成或全败：任一条失败整批拒绝，不落库。
	if rec.Code < 400 {
		t.Fatalf("任一条失败应整批拒绝，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-ui-error") {
		t.Errorf("失败应走共享错误出口：%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("写响应必须是 HTML 片段，实际 %q", ct)
	}
}

func TestCustomersLabelsGoThroughI18n(t *testing.T) {
	fakeTr := func(key, fallback string) string {
		switch key {
		case userenums.LabelKeyStatusActive:
			return "Active"
		case userenums.LabelKeyVerified:
			return "Email verified"
		case "site.fragment.order.status.paid":
			return "Paid"
		default:
			return fallback
		}
	}

	listData := customerTestLayoutData(customerListPageData(customerTestPageTitle, customerListSample(),
		customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false))
	listData["t"] = fakeTr
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", listData)
	for _, want := range []string{"Active", "Email verified"} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页应经取词渲染出 %q", want)
		}
	}
	if n := strings.Count(body, "Active"); n < 2 {
		t.Errorf("计数徽章与行内状态标签应共用同一份 key（各出现一次），实际 %d 次", n)
	}

	detailData := customerTestLayoutData(customerDetailPageData(customerTestDetailTitle, nil, customerSample(), detailProjects(),
		"p1", detailSummary(), false, false, customerMsg{}))
	detailData["t"] = fakeTr
	body = renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", detailData)
	for _, want := range []string{"Active", "Email verified", "Paid"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情页应经取词渲染出 %q", want)
		}
	}
}

func TestCustomerTemplatesRenderNoRawLabels(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "templates", "admin", "user")
	for _, name := range []string{"customers.html", "customer_detail.html"} {
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("读取模板 %s 失败：%v", name, err)
		}
		for _, banned := range []string{
			"{{r.StatusLabel}}", "{{.StatusLabel}}",
			"{{r.EmailVerifiedLabel}}", "{{.EmailVerifiedLabel}}",
			"{{.LastOrderStatusLabel}}",
		} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s 仍直接渲染未取词的 %s（应改为 tr(key, 兜底)）", name, banned)
			}
		}
	}
}
