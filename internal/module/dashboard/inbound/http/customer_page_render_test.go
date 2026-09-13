package dashboardhttp

// customer_page_render_test.go — 客户管理列表页的渲染与 handler 冒烟（后台客户管理）。
//
// 钉住三件事：
//   - admin/customers.html 的 Jet 语法与 layout 数据契约成立（模板错了只会在运营点开时 500）；
//   - 服务端给的字段真的渲染出来了（不是「handler 对了、页面空白」）；
//   - **直接打 handler**（而不是绕过它去渲染模板）—— 模板名与文件对不上时响应是
//     「200 + 空 body」，只测模板本身的测试看不见它（文章页那次真实缺陷就是这么漏的）。
//
// 详情页的用例在 customer_detail_page_render_test.go。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/internal/templates"
)

// —— 替身：后台客户契约（四条方法）与订单聚合（一条方法）——

type fakeCustomerAdmin struct {
	list       *userdto.CustomerListResp
	detail     *userdto.CustomerResp
	err        error
	statusReq  *userdto.CustomerStatusReq
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

func (f *fakeCustomerAdmin) SetCustomerStatus(_ context.Context, req *userdto.CustomerStatusReq) (*userdto.CustomerStatusResp, error) {
	f.statusReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &userdto.CustomerStatusResp{CustomerID: req.CustomerID, Status: req.Status, StatusLabel: "已停用"}, nil
}

func (f *fakeCustomerAdmin) UnlockCustomer(_ context.Context, req *userdto.CustomerUnlockReq) (*userdto.CustomerUnlockResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.unlockRes != nil {
		return f.unlockRes, nil
	}
	return &userdto.CustomerUnlockResp{CustomerID: req.CustomerID, Unlocked: true}, nil
}

var _ usercontract.CustomerAdminPort = (*fakeCustomerAdmin)(nil)

type fakeOrderSummaryReader struct {
	res *orderdto.CustomerOrderSummaryResp
	err error
}

func (f fakeOrderSummaryReader) CustomerOrderSummaryOf(_ context.Context, _ *orderdto.CustomerOrderSummaryReq) (*orderdto.CustomerOrderSummaryResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

var _ ordercontract.CustomerOrderSummaryReader = fakeOrderSummaryReader{}

// fakeProjects 只实现 List：嵌入接口即可满足 ProjectService 的其余方法
// （测试不会调用它们，也不必为此写二十个空实现把测试意图淹掉）。
type fakeProjects struct {
	projectcontract.ProjectService
	items []projectcontract.ProjectResp
}

func (f fakeProjects) List(_ context.Context) ([]projectcontract.ProjectResp, error) {
	return f.items, nil
}

// customerSample 一个客户样本（「已验证 + 未锁定」的常规形态）。
func customerSample() *userdto.CustomerResp {
	registered := time.Date(2026, 9, 1, 10, 30, 0, 0, time.Local)
	lastLogin := time.Date(2026, 9, 20, 8, 5, 0, 0, time.Local)
	return &userdto.CustomerResp{
		ID: 42, Username: "alice", Email: "alice@example.com",
		Nickname: "小艾", DisplayName: "艾丽丝",
		Status: 1, StatusLabel: "正常", EmailVerified: true,
		RegisteredAt: &registered, RegisteredAtText: "2026-09-01 10:30",
		RegisterIP: "203.0.113.7", RegisterLocation: "中国 上海",
		LastLoginTime: &lastLogin, LastLoginTimeText: "2026-09-20 08:05",
		LastLoginIP: "203.0.113.9", LastLoginLocation: "中国 北京",
	}
}

func customerListSample() *userdto.CustomerListResp {
	return &userdto.CustomerListResp{
		List:     []*userdto.CustomerResp{customerSample()},
		Total:    1,
		Counters: userdto.CustomerCounters{Total: 1, Active: 1, Verified: 1},
	}
}

// newCustomerTestEngine 只挂客户页的四条 handler（不经中间件：这里测的是页面本身）。
func newCustomerTestEngine(h *customerPageHandle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/customers", h.CustomersPage)
	engine.GET("/admin/customers/detail", h.CustomerDetailPage)
	engine.POST("/admin/customers/status", h.CustomerStatusSave)
	engine.POST("/admin/customers/unlock", h.CustomerUnlock)
	return engine
}

// —— 列表页模板 ——

func TestCustomersListTemplateRenders(t *testing.T) {
	data := customerListPageData(customerListSample(), customerFilter{
		Keyword: "alice", Status: customerStatusAll,
	}, 1, 20, "", "", false)
	body := renderAdminTemplate(t, "admin/customers.html", articleLayoutData(data))

	for _, want := range []string{
		"客户管理", "艾丽丝", "alice", "alice@example.com", "正常", "已验证",
		"2026-09-01 10:30", "2026-09-20 08:05", "中国 北京",
		"/admin/customers/detail?id=42", "/admin/customers/status",
		"邮箱已验证 1", "user:customer_status",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页渲染结果缺少 %q", want)
		}
	}
}

// TestCustomersListTemplateHidesStatusActionForPending 待激活账号不给「停用」按钮。
//
// 这类账号本来就登不上去（未完成邮箱验证），停用只会让客户点验证链接时得到
// 「链接无效」—— 一个必然把客户引到「为什么我的链接坏了」的动作，不该出现在页面上。
func TestCustomersListTemplateHidesStatusActionForPending(t *testing.T) {
	item := customerSample()
	item.Status = 2
	item.StatusLabel = "待激活"
	item.EmailVerified = false
	list := &userdto.CustomerListResp{List: []*userdto.CustomerResp{item}, Total: 1,
		Counters: userdto.CustomerCounters{Total: 1, Pending: 1, Unverified: 1}}

	body := renderAdminTemplate(t, "admin/customers.html", articleLayoutData(
		customerListPageData(list, customerFilter{Status: customerStatusAll}, 1, 20, "", "", false)))

	if strings.Contains(body, "/admin/customers/status") {
		t.Errorf("待激活账号不应渲染停用按钮")
	}
	if !strings.Contains(body, "客户还没完成邮箱验证") {
		t.Errorf("待激活账号应当有说明文案")
	}
}

// TestCustomersListTemplateRendersUnlockForLocked 锁定账号给「解除锁定」。
func TestCustomersListTemplateRendersUnlockForLocked(t *testing.T) {
	item := customerSample()
	item.Locked = true
	item.LoginFailureCount = 5
	until := time.Date(2026, 9, 20, 9, 0, 0, 0, time.Local)
	item.LockedUntilTime = &until
	item.LockedUntilText = "2026-09-20 09:00"
	list := &userdto.CustomerListResp{List: []*userdto.CustomerResp{item}, Total: 1,
		Counters: userdto.CustomerCounters{Total: 1, Active: 1, Locked: 1}}

	body := renderAdminTemplate(t, "admin/customers.html", articleLayoutData(
		customerListPageData(list, customerFilter{Status: customerStatusAll}, 1, 20, "", "", false)))

	for _, want := range []string{"/admin/customers/unlock", "已锁定", "2026-09-20 09:00", "连续登录失败"} {
		if !strings.Contains(body, want) {
			t.Errorf("锁定账号渲染结果缺少 %q", want)
		}
	}
}

// TestCustomersListTemplateEmptyState 没有数据时给明确说法，而不是空表格。
func TestCustomersListTemplateEmptyState(t *testing.T) {
	body := renderAdminTemplate(t, "admin/customers.html", articleLayoutData(
		customerListPageData(nil, customerFilter{Status: customerStatusAll}, 1, 20, "", "", false)))
	if !strings.Contains(body, "没有符合条件的客户") {
		t.Errorf("空列表应当给出说明文案")
	}
}

// TestCustomersListTemplateCapabilityMissing 能力未装配时给说明，不渲染必然失败的按钮。
func TestCustomersListTemplateCapabilityMissing(t *testing.T) {
	body := renderAdminTemplate(t, "admin/customers.html", articleLayoutData(
		customerListPageData(nil, customerFilter{Status: customerStatusAll}, 1, 20,
			customerUnavailableText, "", true)))
	for _, want := range []string{customerUnavailableText, "装配问题"} {
		if !strings.Contains(body, want) {
			t.Errorf("能力未装配时应渲染 %q", want)
		}
	}
}

// —— 直接打 handler（钉住模板名与参数解析）——

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

// TestCustomersPageHandlerWhitelistsQueryText ?err= 是用户可编辑的，不能原样显示。
func TestCustomersPageHandlerWhitelistsQueryText(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()},
		fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/customers?err="+url.QueryEscape("<script>alert(1)</script>"), nil))

	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("查询参数里的脚本被原样渲染到页面上")
	}
	if !strings.Contains(body, "系统内部错误") {
		t.Errorf("未命中白名单的提示应落到统一文案")
	}
}

// TestCustomersPageHandlerFiltersAreParsed 筛选参数真的进了请求：
// 空 status 必须是「全部」而不是「只看已停用」；结束日期必须覆盖当天。
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
	if last := fake.lastListRe.RegisteredTo.Hour(); last != 23 {
		t.Errorf("结束日期应扩到当天最后一刻，实际 %02d 时", last)
	}
}

// —— 状态写（POST）——

// TestCustomersStatusSaveUsesToStatusAndKeepsFilters 目标状态取自 toStatus，
// 而 status 只用于保留筛选 —— 两者同名会让回跳后的列表看起来「筛选没了」。
func TestCustomersStatusSaveUsesToStatusAndKeepsFilters(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{
		"customerId": {"42"}, "toStatus": {"0"},
		"keyword": {"alice"}, "status": {"1"}, "page": {"2"}, "limit": {"20"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/customers/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("写操作应 302 回列表页，实际 %d", rec.Code)
	}
	if fake.statusReq == nil || fake.statusReq.Status != customerStatusDisabled {
		t.Fatalf("目标状态没有从 toStatus 解析出来：%+v", fake.statusReq)
	}
	loc := rec.Header().Get("Location")
	for _, want := range []string{"keyword=alice", "status=1", "ok="} {
		if !strings.Contains(loc, want) {
			t.Errorf("回跳地址缺少 %q：%s", want, loc)
		}
	}
}

// TestCustomerStatusSaveRedirectsToDetail 带 customerId 的表单回详情页
// （运营是在那个客户页面上点的按钮，把他弹回列表第一页等于让他重新找一遍）。
func TestCustomerStatusSaveRedirectsToDetail(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"customerId": {"42"}, "toStatus": {"1"}, "project": {"p1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/customers/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/customers/detail?") {
		t.Errorf("应回详情页，实际 %s", loc)
	}
}

// TestCustomerStatusSaveRejectsUnknownTarget 非法目标状态一律拒绝，且根本不调 service。
func TestCustomerStatusSaveRejectsUnknownTarget(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	// 2 = 待激活：它是注册流程的中间态，不是后台能设置的目标状态。
	form := url.Values{"customerId": {"42"}, "toStatus": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/customers/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if fake.statusReq != nil {
		t.Fatalf("非法目标状态不应调用 service：%+v", fake.statusReq)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("应当带错误提示回跳，实际 %s", loc)
	}
}

// TestCustomerUnlockDistinguishesOutcomes 三种解锁结果各有各的说法：
// 「刚解锁」「没锁但清了失败计数」「本来就没事」—— 合成一句会让运营以为按钮坏了。
func TestCustomerUnlockDistinguishesOutcomes(t *testing.T) {
	cases := []struct {
		name string
		res  *userdto.CustomerUnlockResp
		want string
	}{
		{"真的解除了", &userdto.CustomerUnlockResp{CustomerID: 42, Unlocked: true}, "解除锁定"},
		{"清了残留计数", &userdto.CustomerUnlockResp{CustomerID: 42, Cleared: true}, "失败计数已清零"},
		{"本来就没事", &userdto.CustomerUnlockResp{CustomerID: 42}, "没有处于锁定状态"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewCustomerPageHandle(&fakeCustomerAdmin{unlockRes: tc.res}, fakeOrderSummaryReader{}, fakeProjects{})
			engine := newCustomerTestEngine(h)

			form := url.Values{"customerId": {"42"}}
			req := httptest.NewRequest(http.MethodPost, "/admin/customers/unlock", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			loc := rec.Header().Get("Location")
			if !strings.Contains(loc, "ok=") {
				t.Fatalf("解锁后应带成功回执，实际 %s", loc)
			}
			if !strings.Contains(loc, tc.want) && !strings.Contains(loc, url.QueryEscape(tc.want)) {
				t.Errorf("回执文案应含 %q，实际 %s", tc.want, loc)
			}
		})
	}
}
