package userhttp

// customer_detail_page_render_test.go — 客户详情页的渲染与 handler 冒烟。
//
// 详情页有一块**跨模块拼装**的内容：客户资料来自 user 模块，订单摘要来自 order 模块
//（只读聚合）。这块最容易出现「看起来正常其实不可用」的状态 ——
// 没有工程 / 订单能力没装配 / 摘要查询失败，三种都要有自己的说法，
// 而不是渲染一片空白让人以为「这个客户没下过单」。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

// detailSummary 一个正常的订单摘要（已付款 2 单、累计 198 元、最近一单 SO20260920001）。
func detailSummary() *orderdto.CustomerOrderSummaryResp {
	return &orderdto.CustomerOrderSummaryResp{
		UserID: 42, ProjectID: "p1",
		OrderCount: 3, PaidOrderCount: 2, TotalAmount: 19800, TotalAmountLabel: "198.00",
		LastOrderID: 900, LastOrderNo: "SO20260920001", LastOrderStatus: "paid",
		LastOrderTimeText: "2026-09-20 08:05", HasOrders: true,
	}
}

func detailProjects() []projectcontract.ProjectResp {
	return []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}}
}

func TestCustomerDetailTemplateRenders(t *testing.T) {
	data := customerDetailPageData(customerSample(), detailProjects(), "p1", detailSummary(),
		false, false, "", "", nil)
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	for _, want := range []string{
		"客户详情", "艾丽丝", "alice@example.com", "203.0.113.7", "203.0.113.9",
		"官网", "订单数 3", "¥198.00", "SO20260920001",
		// 只断言参数本身：url.Values.Encode 会按字母序重排，且 Jet 会把 & 转义成 &amp;，
		// 断言整串等于把实现细节抄进测试（改一次排序就要改一次测试）。
		"orderId=900", "/admin/orders?",
		"/admin/customers/status", "累计消费只统计",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("详情页渲染结果缺少 %q", want)
		}
	}
}

// TestCustomerDetailTemplateNoOrders 没下过单是正常状态，不是错误。
func TestCustomerDetailTemplateNoOrders(t *testing.T) {
	summary := &orderdto.CustomerOrderSummaryResp{UserID: 42, ProjectID: "p1"}
	data := customerDetailPageData(customerSample(), detailProjects(), "p1", summary,
		false, false, "", "", nil)
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if !strings.Contains(body, "还没有下过单") {
		t.Errorf("零订单时应显示「还没有下过单」")
	}
	if strings.Contains(body, "订单摘要暂时读不出来") {
		t.Errorf("零订单不是错误，不应显示读取失败提示")
	}
}

// TestCustomerDetailTemplateNoProjects 没有站点工程时给说明，而不是渲染一个没用的工程下拉。
func TestCustomerDetailTemplateNoProjects(t *testing.T) {
	data := customerDetailPageData(customerSample(), nil, "", nil, false, false, "", "", nil)
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if !strings.Contains(body, "还没有站点工程，因此没有订单可统计") {
		t.Errorf("无工程时应给出说明")
	}
	if strings.Contains(body, "<select class=\"form-select\" name=\"project\"") {
		t.Errorf("无工程时不应渲染工程下拉")
	}
}

// TestCustomerDetailTemplateSummaryFailed 摘要读不出来时给说明，客户资料照常显示。
func TestCustomerDetailTemplateSummaryFailed(t *testing.T) {
	data := customerDetailPageData(customerSample(), detailProjects(), "p1", nil,
		false, true, "", "", nil)
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if !strings.Contains(body, "订单摘要暂时读不出来") {
		t.Errorf("摘要失败时应给出说明")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Errorf("摘要失败不应影响客户资料的渲染")
	}
}

// TestCustomerDetailTemplatePendingHasNoStatusAction 待激活客户的详情页不给停用按钮。
func TestCustomerDetailTemplatePendingHasNoStatusAction(t *testing.T) {
	item := customerSample()
	item.Status = 2
	item.StatusLabel = "待激活"
	data := customerDetailPageData(item, nil, "", nil, false, false, "", "", nil)
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if strings.Contains(body, "/admin/customers/status") {
		t.Errorf("待激活客户不应渲染停用按钮")
	}
	if !strings.Contains(body, "客户还没完成邮箱验证") {
		t.Errorf("待激活客户应有解释文案")
	}
}

// TestCustomerDetailPageHandlerRendersOrderSummary 直接打 handler（钉住模板名）。
func TestCustomerDetailPageHandlerRendersOrderSummary(t *testing.T) {
	h := NewCustomerPageHandle(
		&fakeCustomerAdmin{detail: customerSample()},
		fakeOrderSummaryReader{res: detailSummary()},
		fakeProjects{items: detailProjects()})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail?id=42", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("客户详情页返回 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("客户详情页响应体为空 —— handler 里的模板名很可能与模板文件对不上")
	}
	for _, want := range []string{"SO20260920001", "¥198.00", "alice@example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情页缺少 %q", want)
		}
	}
}

// TestCustomerDetailPageRedirectsWhenMissing 客户不存在时回列表页并带原因，不渲染空详情页
// （详情页的每个动作都要求一个存在的客户，渲染出来只是一堆必然失败的按钮）。
func TestCustomerDetailPageRedirectsWhenMissing(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: nil}, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail?id=42", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("客户不存在时应 302 回列表页，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "/admin/customers?") {
		t.Errorf("回跳地址不对：%s", rec.Header().Get("Location"))
	}
}

// TestCustomerDetailPageWithoutIDRedirects 没带 id 一律回列表页（不渲染一个「空客户」）。
func TestCustomerDetailPageWithoutIDRedirects(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: customerSample()}, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("没带 id 时应 302 回列表页，实际 %d", rec.Code)
	}
}
