package userhttp

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

	"github.com/gin-gonic/gin"
	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/utils"
)

// customerTestLayoutData 补齐 layout 需要的键（与线上 shell.Prepare 注入的一致）。
func customerTestLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/customers"
	return base
}

// renderCustomerAdminTemplate 用真实 Jet 渲染器渲染一个后台模板并返回 HTML。
func renderCustomerAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/user/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 渲染失败，状态 %d", name, rec.Code)
	}
	return rec.Body.String()
}

// —— 替身：后台客户契约（四条方法）与订单聚合（一条方法）——

type fakeCustomerAdmin struct {
	list   *userdto.CustomerListResp
	detail *userdto.CustomerResp
	err    error
	// failIDs 这些 id 的单条写入失败（批量用例靠它构造「一条失败、其余照常」）。
	failIDs map[uint64]bool
	// unlockByID 按 id 指定解锁结果（不设则用 unlockRes / 默认「刚解锁」）。
	unlockByID map[uint64]*userdto.CustomerUnlockResp
	statusReq  *userdto.CustomerStatusReq
	// statusIDs 依次收到的 id：钉住「批量确实逐条走了单条路径」。
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
		RegisteredAt: utils.NewJSONTimePtr(&registered), RegisteredAtText: "2026-09-01 10:30",
		RegisterIP: "203.0.113.7", RegisterLocation: "中国 上海",
		LastLoginTime: utils.NewJSONTimePtr(&lastLogin), LastLoginTimeText: "2026-09-20 08:05",
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
	engine.GET("/admin/customers/overview", h.CustomerOverviewPage)
	engine.GET("/admin/customers/detail", h.CustomerDetailPage)
	engine.POST("/admin/customers/status", h.CustomerStatusSave)
	engine.POST("/admin/customers/unlock", h.CustomerUnlock)
	engine.POST("/admin/customers/bulk-status", h.CustomerBulkStatusSave)
	engine.POST("/admin/customers/bulk-unlock", h.CustomerBulkUnlock)
	return engine
}

// —— 列表页模板 ——

func TestCustomersListTemplateRenders(t *testing.T) {
	data := customerListPageData(nil, customerListSample(), customerFilter{
		Keyword: "alice", Status: customerStatusAll,
	}, 1, 20, "", false)
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(data))

	for _, want := range []string{
		"客户管理", "艾丽丝", "alice", "alice@example.com", "正常", "已验证",
		"2026-09-01 10:30", "2026-09-20 08:05", "中国 北京",
		"/admin/customers/detail?id=42", "/admin/customers/status",
		"邮箱已验证 1", "user:customer_status",
		// 首列勾选 + 批量条 + 两个批量端点（缺一整套批量都是摆设）
		`action="/admin/customers/bulk-status?`, "/admin/customers/bulk-unlock",
		"data-check-all", "data-check-item", "data-bulk-bar", `name="ids" value="42"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页渲染结果缺少 %q", want)
		}
	}

	// 结构顺序：批量 form 包住表格，行内写操作表单必须在它**外面**。
	// HTML 不允许 form 嵌套 —— 嵌套时解析器会丢掉内层 form 标签，
	// 表现是「行内的停用 / 解除锁定按钮点了没反应」，且只有真实浏览器才暴露。
	bulkForm := strings.Index(body, `action="/admin/customers/bulk-status?`)
	table := strings.Index(body, `customers-page-table`)
	rowForm := strings.Index(body, `id="customer-status-42"`)
	if bulkForm < 0 || table < 0 || rowForm < 0 || !(bulkForm < table && table < rowForm) {
		t.Errorf("批量 form / 表格 / 行内表单的顺序不对：bulk=%d table=%d row=%d", bulkForm, table, rowForm)
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

	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(nil, list, customerFilter{Status: customerStatusAll}, 1, 20, "", false)))

	if strings.Contains(body, "/admin/customers/status") {
		t.Errorf("待激活账号不应渲染停用按钮")
	}
	// 状态说明在**表头**的 .help 里（一次渲染、不随行重复），不再逐行插整行 colspan 说明行。
	// 判据：表头段含这句说明；tbody 段里没有任何跨列行（有数据时 tbody 的每一行都是 7 个 td
	// 的数据行）。原来这里断言 `colspan="7"` 存在 —— 那是「逐行说明行」的形态，已撤掉。
	head := body[:strings.Index(body, "<tbody")]
	if !strings.Contains(head, "客户还没完成邮箱验证") {
		t.Errorf("状态列的解释应挂在表头 .help 里")
	}
	tbody := body[strings.Index(body, "<tbody"):]
	if strings.Contains(tbody, "colspan=") {
		t.Errorf("有数据时 tbody 不应有跨列说明行（说明已移进状态列表头的 .help）")
	}
}

// TestCustomersListTemplateRendersUnlockForLocked 锁定账号给「解除锁定」。
func TestCustomersListTemplateRendersUnlockForLocked(t *testing.T) {
	item := customerSample()
	item.Locked = true
	item.LoginFailureCount = 5
	until := time.Date(2026, 9, 20, 9, 0, 0, 0, time.Local)
	item.LockedUntilTime = utils.NewJSONTimePtr(&until)
	item.LockedUntilText = "2026-09-20 09:00"
	list := &userdto.CustomerListResp{List: []*userdto.CustomerResp{item}, Total: 1,
		Counters: userdto.CustomerCounters{Total: 1, Active: 1, Locked: 1}}

	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(nil, list, customerFilter{Status: customerStatusAll}, 1, 20, "", false)))

	for _, want := range []string{"/admin/customers/unlock", "已锁定", "2026-09-20 09:00", "连续登录失败"} {
		if !strings.Contains(body, want) {
			t.Errorf("锁定账号渲染结果缺少 %q", want)
		}
	}
}

// TestCustomersListTemplateEmptyState 没有数据时给明确说法，而不是空表格。
//
// 两种空要分开说（这是「徽章即筛选」的直接后果）：点「已停用 0」进来看到的是
// 「该筛选条件下暂时没有账号」，而站点一个客户都没有时是「还没有客户」——
// 用同一句话兜住，运营会以为站点里没人注册过（admin-ui-logic §7）。
func TestCustomersListTemplateEmptyState(t *testing.T) {
	plain := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(nil, nil, customerFilter{Status: customerStatusAll}, 1, 20, "", false)))
	if !strings.Contains(plain, "还没有客户") {
		t.Errorf("无筛选的空列表应当说明「还没有客户」")
	}

	filtered := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(nil, nil, customerFilter{Status: customerStatusDisabled}, 1, 20, "", false)))
	if !strings.Contains(filtered, "该筛选条件下暂时没有账号") {
		t.Errorf("带筛选的空列表应当说明「该筛选条件下暂时没有账号」")
	}
}

// TestCustomersListTemplateCapabilityMissing 能力未装配时给说明，不渲染必然失败的按钮。
func TestCustomersListTemplateCapabilityMissing(t *testing.T) {
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(nil, nil, customerFilter{Status: customerStatusAll}, 1, 20,
			customerUnavailableLabel.fallback, true)))
	for _, want := range []string{customerUnavailableLabel.fallback, "装配问题"} {
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

// TestCustomersPageIgnoresForgedNoticeQuery 写结论不再经查询参数回显：手拼 ?err= / ?ok= /
// ?done= 一律不出现在页面上（读侧判定已随「结论走 shell.RenderJump」整批删除）。
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
	if last := fake.lastListRe.RegisteredTo.Time().Hour(); last != 23 {
		t.Errorf("结束日期应扩到当天最后一刻，实际 %02d 时", last)
	}
}

// —— 状态写（POST）——

// TestCustomersStatusSaveRendersJumpAndKeepsFilters 目标状态取自 toStatus；成功走整页提示
// （HTTP 200 + data-jump-state="ok"），回跳地址从表单 action 的 query 读回筛选上下文。
func TestCustomersStatusSaveRendersJumpAndKeepsFilters(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"customerId": {"42"}, "toStatus": {"0"}}
	req := httptest.NewRequest(http.MethodPost,
		"/admin/customers/status?keyword=alice&status=1&page=2&limit=20",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("写操作应渲染提示页（200），实际 %d", rec.Code)
	}
	if fake.statusReq == nil || fake.statusReq.Status != customerStatusDisabled {
		t.Fatalf("目标状态没有从 toStatus 解析出来：%+v", fake.statusReq)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) {
		t.Errorf(`成功提示页应有 data-jump-state="ok"`)
	}
	if !strings.Contains(body, "账号已停用") {
		t.Errorf("成功提示页应含回执文案")
	}
	for _, want := range []string{"keyword=alice", "status=1", "page=2"} {
		if !strings.Contains(body, want) {
			t.Errorf("回跳地址缺少 %q", want)
		}
	}
}

// TestCustomerStatusSaveFromDetailReturnsToDetail 详情页表单（action 的 query 带 id）的
// 提示页回跳目标是详情页 —— 运营是在那个客户页面上点的按钮，把他弹回列表第一页
// 等于让他重新找一遍那个客户。
func TestCustomerStatusSaveFromDetailReturnsToDetail(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"customerId": {"42"}, "toStatus": {"1"}}
	req := httptest.NewRequest(http.MethodPost,
		"/admin/customers/status?id=42&project=p1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("应渲染提示页（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) {
		t.Errorf(`成功提示页应有 data-jump-state="ok"`)
	}
	if !strings.Contains(body, "/admin/customers/detail?id=42") {
		t.Errorf("应回详情页，实际响应未含详情链接")
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
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Errorf(`应当渲染失败提示页，实际状态 %d`, rec.Code)
	}
	if !strings.Contains(body, "账号状态取值不合法") {
		t.Errorf("提示页应含「账号状态取值不合法」")
	}
}

// TestCustomerUnlockDistinguishesOutcomes 三种解锁结果各有各的说法：
// 「刚解锁」「没锁但清了失败计数」「本来就没事」—— 合成一句会让运营以为按钮坏了。
func TestCustomerUnlockDistinguishesOutcomes(t *testing.T) {
	cases := []struct {
		name string
		res  *userdto.CustomerUnlockResp
		// want 是词条缺失时回落的中文兜底（单测不起库，取词函数据 fallback 原样返回）。
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
			req := httptest.NewRequest(http.MethodPost, "/admin/customers/unlock", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("解锁后应渲染提示页（200），实际 %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `data-jump-state="ok"`) {
				t.Errorf(`解锁后应有 data-jump-state="ok"`)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("回执文案应含 %q", tc.want)
			}
		})
	}
}

// TestCustomersBulkStatusRejectsUnknownTarget 与单条动作同口径：
// 非法目标状态（待激活是注册流程的中间态）一律拒绝，且一条都不写。
func TestCustomersBulkStatusRejectsUnknownTarget(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"ids": {"42", "43"}, "toStatus": {"2"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/customers/bulk-status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if len(fake.statusIDs) != 0 {
		t.Fatalf("非法目标状态不应调用 service：%v", fake.statusIDs)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Errorf("应当渲染失败提示页，实际状态 %d", rec.Code)
	}
	if !strings.Contains(body, "账号状态取值不合法") {
		t.Errorf("提示页应含「账号状态取值不合法」")
	}
}

// TestCustomersBulkNothingSelected 一条都没勾就提交：不当成功处理（表单是客户端可控的）。
func TestCustomersBulkNothingSelected(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"toStatus": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/customers/bulk-status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if len(fake.statusIDs) != 0 {
		t.Fatalf("没有勾选时不应调用 service：%v", fake.statusIDs)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Errorf("没有勾选应渲染失败提示页，实际状态 %d", rec.Code)
	}
	if !strings.Contains(body, "没有勾选任何账号") {
		t.Errorf("没有勾选应给出提示")
	}
}

// TestCustomersBulkUnlockDistinguishesNoop 「本来就没事」的账号不计入「已解除锁定」：
// 混进成功是谎报，混进失败会让运营点第二次（而第二次结果一模一样）。
func TestCustomersBulkUnlockDistinguishesNoop(t *testing.T) {
	fake := &fakeCustomerAdmin{unlockByID: map[uint64]*userdto.CustomerUnlockResp{
		42: {CustomerID: 42, Unlocked: true},
		43: {CustomerID: 43},
	}}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"ids": {"42", "43"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/customers/bulk-unlock", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("批量解锁应渲染提示页（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) {
		t.Errorf(`批量解锁应有 data-jump-state="ok"`)
	}
	for _, want := range []string{"已解除锁定 1 个", "1 个本来就未锁定"} {
		if !strings.Contains(body, want) {
			t.Errorf("提示页应含 %q", want)
		}
	}
}

// —— 计数徽章即筛选（同一维度只给一种控件）——

// TestCustomersCounterTabsAreClickableFilters 徽章是链接，URL 由服务端生成：
// 只动自己那个维度，其他条件保留；当前生效的那个带 aria-current 与图标。
func TestCustomersCounterTabsAreClickableFilters(t *testing.T) {
	data := customerListPageData(nil, customerListSample(),
		customerFilter{Status: customerStatusActive}, 1, 20, "", false)
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(data))

	for _, want := range []string{"aria-current=\"true\"", "✓"} {
		if !strings.Contains(body, want) {
			t.Errorf("徽章缺少选中态标记 %q", want)
		}
	}
	// 链接按解析后的形态比对：Jet 会把属性值里的 & 转义（&amp; / &#38;），
	// 断言原文会把「转义正确」这件事误判成「链接错了」。
	hrefs := map[string]bool{}
	for _, m := range regexp.MustCompile(`href="(/admin/customers[^"]*)"`).FindAllStringSubmatch(body, -1) {
		raw := strings.ReplaceAll(m[1], "&amp;", "&")
		raw = strings.ReplaceAll(raw, "&#38;", "&")
		hrefs[raw] = true
	}
	for _, want := range []string{
		"/admin/customers",                          // 全部：不带任何维度取值
		"/admin/customers?status=0",                 // 已停用：只改状态维度
		"/admin/customers?locked=1&status=1",        // 已锁定：独立维度，状态保留
		"/admin/customers?emailVerified=2&status=1", // 邮箱未验证：状态保留
	} {
		if !hrefs[want] {
			t.Errorf("徽章链接缺少 %q，实际 %v", want, hrefs)
		}
	}
	// 同一维度不能同时存在徽章与下拉：两套控件表达一个维度时，用户无法判断它们是否等价。
	for _, gone := range []string{
		`id="customers-filter-status"`,
		`id="customers-filter-verified"`,
	} {
		if strings.Contains(body, gone) {
			t.Errorf("状态 / 邮箱维度应当只剩徽章，不应再有下拉：%s", gone)
		}
	}
}

// TestCustomersPageHandlerParsesLockedFilter 「已锁定」是独立维度，必须真的进查询
// （锁定只写 locked_until_time，账号状态仍是「正常」，拿状态筛选表达不了）。
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

// —— 批量动作（POST /admin/customers/bulk-status、/admin/customers/bulk-unlock）——

// TestCustomersBulkStatusKeepsGoingAfterOneFailure 单条失败不中断整批。
//
// 整批回滚是这里最要命的错误实现：运营看到「一条都没做」会反复重试，
// 而每次重试都会把已经成功的那些再做一遍（重复写库、update_time 反复变化）。
// 结论渲染进提示页（HTTP 200），回跳地址从表单 action 的 query 读回筛选上下文。
func TestCustomersBulkStatusKeepsGoingAfterOneFailure(t *testing.T) {
	fake := &fakeCustomerAdmin{failIDs: map[uint64]bool{7: true}}
	h := NewCustomerPageHandle(fake, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	form := url.Values{"ids": {"42", "7", "43"}, "toStatus": {"0"}}
	req := httptest.NewRequest(http.MethodPost,
		"/admin/customers/bulk-status?keyword=alice&page=2", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("批量动作应渲染提示页（200），实际 %d", rec.Code)
	}
	if len(fake.statusIDs) != 3 {
		t.Fatalf("三条都应逐条走到单条写入路径，实际 %v", fake.statusIDs)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) {
		t.Errorf("批量完成应渲染成功提示页")
	}
	for _, want := range []string{"已停用 2 个", "1 个未处理"} {
		if !strings.Contains(body, want) {
			t.Errorf("提示页应含结果摘要 %q", want)
		}
	}
	for _, want := range []string{"keyword=alice", "page=2"} {
		if !strings.Contains(body, want) {
			t.Errorf("回跳应保留筛选 %q", want)
		}
	}
}

// —— 状态 / 邮箱验证 / 订单状态标签的取词（本次重构的主验收点）——

// TestCustomersLabelsGoThroughI18n 标签必须经取词渲染，而不是直接渲染服务端给的中文。
//
// 为什么用**假取词函数**而不是真 i18n：模块内单测不起库，TranslateFunc 会走中文兜底 ——
// 那正好与「模板硬编码中文」的表现一模一样，压根分不出对错。假函数按 key 返回可辨认的
// 英文，一旦模板改回直接渲染中文字段（不走 tr），页面里就不会出现这些英文，当场红。
//
// 顺带钉住「计数徽章与行内状态标签共用同一份 key 来源」：同一个 key 在列表页出现两次
// （徽章一次、行一次），只改其中一处（比如徽章自己留一份 key）会让计数变成 1。
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

	listData := customerTestLayoutData(customerListPageData(nil, customerListSample(),
		customerFilter{Status: customerStatusAll}, 1, 20, "", false))
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

	detailData := customerTestLayoutData(customerDetailPageData(customerSample(), detailProjects(),
		"p1", detailSummary(), false, false, "", nil))
	detailData["t"] = fakeTr
	body = renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", detailData)
	for _, want := range []string{"Active", "Email verified", "Paid"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情页应经取词渲染出 %q", want)
		}
	}
}

// TestCustomerTemplatesRenderNoRawLabels 模板不得再直接渲染未取词的标签字段。
//
// 断言模板**源文本**（而不是渲染结果）：渲染结果在中文下与被禁止的写法长得一样，
// 只有源文本能区分「走了取词但兜底是中文」与「压根没走取词」。
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
