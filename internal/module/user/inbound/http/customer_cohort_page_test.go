package userhttp

import (
	"context"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

type fakeCohortReader struct {
	res  *orderdto.CustomerCohortResp
	err  error
	got  *orderdto.CustomerCohortReq
	call int
}

func (f *fakeCohortReader) CustomerCohortByRange(_ context.Context, req *orderdto.CustomerCohortReq) (*orderdto.CustomerCohortResp, error) {
	f.call++
	f.got = req
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func cohortSample() *orderdto.CustomerCohortResp {
	return &orderdto.CustomerCohortResp{
		ProjectID: "11111111-1111-1111-1111-111111111111",
		From:      "2026-07-01", To: "2026-09-30",
		Months: 3, Cohorts: 2, Customers: 3,
		Rows: []orderdto.CustomerCohortRow{
			{
				CohortMonth: "2026-07", CohortLabel: "2026年07月", CohortSize: 2,
				Cells: []orderdto.CustomerCohortCell{
					{MonthIndex: 0, MonthLabel: "2026年07月", ActiveCustomers: 2, RetentionRatePct: 100, RetentionLabel: "100.0%", Reached: true},
					{MonthIndex: 1, MonthLabel: "2026年08月", ActiveCustomers: 1, RetentionRatePct: 50, RetentionLabel: "50.0%", Reached: true},
					{MonthIndex: 2, MonthLabel: "2026年09月", ActiveCustomers: 0, RetentionRatePct: 0, RetentionLabel: "0.0%", Reached: true},
				},
			},
			{
				CohortMonth: "2026-08", CohortLabel: "2026年08月", CohortSize: 1,
				Cells: []orderdto.CustomerCohortCell{
					{MonthIndex: 0, MonthLabel: "2026年08月", ActiveCustomers: 1, RetentionRatePct: 100, RetentionLabel: "100.0%", Reached: true},
					{MonthIndex: 1, MonthLabel: "2026年09月", ActiveCustomers: 1, RetentionRatePct: 100, RetentionLabel: "100.0%", Reached: true},
					{MonthIndex: 2, MonthLabel: "2026年10月", ActiveCustomers: 0, RetentionRatePct: 0, RetentionLabel: "0.0%", Reached: false},
				},
			},
		},
	}
}

func TestCustomerCohortPageRendersMatrix(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerCohort(&fakeCohortReader{res: cohortSample()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/cohort", pageview.Slot(CustomerCohortPageTemplate, pageview.RenderPage), h.CustomerCohortPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/cohort?range=year", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d（标题漏设也是一种 500）", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"群组留存", "2026年07月", "2026年08月", "100.0%", "50.0%",
		"首月", "+1月", "+2月",

		"3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("群组留存页缺少 %q", want)
		}
	}

	if n := strings.Count(body, "<th>") + strings.Count(body, "<th "); n != 5 {
		t.Errorf("表头应为 5 列，实得 %d（判据要锚定标签名，<thead 也以 <th 开头）", n)
	}
}

func TestCustomerCohortPageFutureMonthIsBlank(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerCohort(&fakeCohortReader{res: cohortSample()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/cohort", pageview.Slot(CustomerCohortPageTemplate, pageview.RenderPage), h.CustomerCohortPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/cohort", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "—") {
		t.Error("未到达的月份应显示为占位符（—），而不是 0.0%")
	}

	if n := strings.Count(body, ">0.0%<"); n != 1 {
		t.Errorf("只有「已到达且无人」的那一格才该显示 0.0%%，实得 %d 处", n)
	}

	if n := strings.Count(body, ">—<"); n != 1 {
		t.Errorf("未到达的月份应恰好占 1 格，实得 %d 格", n)
	}
}

func TestCustomerCohortPageWithoutPortShowsNotice(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/cohort", pageview.Slot(CustomerCohortPageTemplate, pageview.RenderPage), h.CustomerCohortPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/cohort", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d（端口缺席不该 500）", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "群组留存暂时不可用") {
		t.Error("端口缺席时应显示「暂时不可用」的说明")
	}

	if !strings.Contains(body, "<table") {
		t.Error("端口缺席时表头仍应保留")
	}
	if strings.Contains(body, "100.0%") {
		t.Error("端口缺席时不该出现任何留存数字")
	}
	if strings.Contains(body, "这段时间里没有新客户") {
		t.Error("端口缺席不等于「这段时间没有新客户」—— 两种空不能共用文案")
	}
}

func TestCustomerCohortPageWithoutProjectsShowsEmpty(t *testing.T) {
	reader := &fakeCohortReader{res: cohortSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{})
	h.SetCustomerCohort(reader)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/cohort", pageview.Slot(CustomerCohortPageTemplate, pageview.RenderPage), h.CustomerCohortPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/cohort", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if reader.call != 0 {
		t.Error("没有工程时不该去查订单模块（那边会直接拒）")
	}
	if !strings.Contains(rec.Body.String(), "这段时间里没有新客户") {
		t.Error("没有工程时应显示空态说明")
	}
}

func TestCustomerCohortPagePassesWindow(t *testing.T) {
	reader := &fakeCohortReader{res: cohortSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerCohort(reader)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/cohort", pageview.Slot(CustomerCohortPageTemplate, pageview.RenderPage), h.CustomerCohortPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/cohort?range=month", nil))
	if reader.got == nil {
		t.Fatal("没有向订单模块取群组留存")
	}
	if reader.got.ProjectID == "" {
		t.Error("必须带上工程（订单侧聚合都要显式工程作用域）")
	}
	if len(reader.got.From) != 10 || len(reader.got.To) != 10 {
		t.Errorf("区间应被解析成 YYYY-MM-DD，实得 %q ~ %q", reader.got.From, reader.got.To)
	}
}
