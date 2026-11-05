package userhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/utils"
)

func detailSummary() *ordercontract.CustomerOrderSummaryResp {
	lastOrder := time.Date(2026, 9, 20, 8, 5, 0, 0, time.Local)
	return &ordercontract.CustomerOrderSummaryResp{
		UserID: 42, ProjectID: "p1",
		OrderCount: 3, PaidOrderCount: 2, TotalAmount: 19800, TotalAmountLabel: "198.00",
		LastOrderID: 900, LastOrderNo: "SO20260920001", LastOrderStatus: "paid",
		LastOrderTime: utils.NewJSONTimePtr(&lastOrder), HasOrders: true,
	}
}

func detailProjects() []projectcontract.ProjectResp {
	return []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}}
}

func TestCustomerDetailTemplateRenders(t *testing.T) {
	data := customerDetailPageData(customerTestDetailTitle, nil, customerSample(), detailProjects(), "p1", detailSummary(),
		false, false, customerMsg{})
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	for _, want := range []string{
		"客户详情", "艾丽丝", "alice@example.com", "203.0.113.7", "203.0.113.9",
		"官网", "订单数 3", "¥198.00", "SO20260920001",

		"2026-09-01 10:30", "2026-09-20 08:05",

		"orderId=900", "/admin/orders?",
		"/api/customer/status", "累计消费只统计",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("详情页渲染结果缺少 %q", want)
		}
	}
}

func TestCustomerDetailTemplateNoOrders(t *testing.T) {
	summary := &ordercontract.CustomerOrderSummaryResp{UserID: 42, ProjectID: "p1"}
	data := customerDetailPageData(customerTestDetailTitle, nil, customerSample(), detailProjects(), "p1", summary,
		false, false, customerMsg{})
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if !strings.Contains(body, "还没有下过单") {
		t.Errorf("零订单时应显示「还没有下过单」")
	}
	if strings.Contains(body, "订单摘要暂时读不出来") {
		t.Errorf("零订单不是错误，不应显示读取失败提示")
	}
}

func TestCustomerDetailTemplateNoProjects(t *testing.T) {
	data := customerDetailPageData(customerTestDetailTitle, nil, customerSample(), nil, "", nil, false, false, customerMsg{})
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if !strings.Contains(body, "还没有站点工程，因此没有订单可统计") {
		t.Errorf("无工程时应给出说明")
	}
	if strings.Contains(body, "<select class=\"form-select\" name=\"project\"") {
		t.Errorf("无工程时不应渲染工程下拉")
	}
}

func TestCustomerDetailTemplateSummaryFailed(t *testing.T) {
	data := customerDetailPageData(customerTestDetailTitle, nil, customerSample(), detailProjects(), "p1", nil,
		false, true, customerMsg{})
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if !strings.Contains(body, "订单摘要暂时读不出来") {
		t.Errorf("摘要失败时应给出说明")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Errorf("摘要失败不应影响客户资料的渲染")
	}
}

func TestCustomerDetailTemplatePendingHasNoStatusAction(t *testing.T) {
	item := customerSample()
	item.Status = 2
	item.StatusLabel = "待激活"
	data := customerDetailPageData(customerTestDetailTitle, nil, item, nil, "", nil, false, false, customerMsg{})
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(data))

	if strings.Contains(body, "/api/customer/status") {
		t.Errorf("待激活客户不应渲染停用按钮")
	}
	if !strings.Contains(body, "客户还没完成邮箱验证") {
		t.Errorf("待激活客户应有解释文案")
	}
}

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

func TestCustomerDetailPageMissingRendersJump(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: nil}, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail?id=42", nil))

	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("客户不存在应给真实 4xx，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Errorf("客户不存在应走共享失败出口的受控错误区：%s", body)
	}
	if strings.Contains(body, "jump-page") {
		t.Errorf("不得再走整页跳转提示：%s", body)
	}
}

func TestCustomerDetailPageWithoutIDRendersJump(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: customerSample()}, fakeOrderSummaryReader{}, fakeProjects{})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("没带 id 应给真实 400，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Errorf("没带 id 应走共享失败出口的受控错误区：%s", body)
	}
}
