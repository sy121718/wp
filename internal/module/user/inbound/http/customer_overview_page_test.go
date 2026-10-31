package userhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

type fakeGrowthReader struct {
	res *orderdto.CustomerGrowthResp
	err error
	got *orderdto.CustomerGrowthReq
}

func (f *fakeGrowthReader) CustomerGrowthByRange(_ context.Context, req *orderdto.CustomerGrowthReq) (*orderdto.CustomerGrowthResp, error) {
	f.got = req
	return f.res, f.err
}

func TestCustomerOverviewRangeOfPresets(t *testing.T) {

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		key      string
		wantFrom string
		wantTo   string
		wantKey  string
	}{
		{"today", "2026-10-05", "2026-10-05", "today"},
		{"yesterday", "2026-10-04", "2026-10-05", "yesterday"},
		{"week", "2026-10-05", "2026-10-05", "week"},
		{"month", "2026-10-01", "2026-10-05", "month"},
		{"year", "2026-01-01", "2026-10-05", "year"},
	}
	for _, c := range cases {
		got := customerOverviewRangeOf(c.key, "", "", now)
		if got.Key != c.wantKey || got.From != c.wantFrom || got.To != c.wantTo {
			t.Errorf("%s: got %+v, want from=%s to=%s key=%s", c.key, got, c.wantFrom, c.wantTo, c.wantKey)
		}
	}
}

func TestCustomerOverviewRangeWeekStartsMonday(t *testing.T) {
	wed := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	got := customerOverviewRangeOf("week", "", "", wed)
	if got.From != "2026-10-05" {
		t.Errorf("周三的「本周」应回溯到周一 2026-10-05，实际 %s", got.From)
	}
}

func TestCustomerOverviewRangeUnknownFallsBack(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, key := range []string{"", "custom", "last-year", "<script>"} {
		got := customerOverviewRangeOf(key, "", "", now)
		if got.Key != customerOverviewDefaultPreset {
			t.Errorf("%q 应回落 %s，实际 %s", key, customerOverviewDefaultPreset, got.Key)
		}
		if got.From != "2026-10-01" {
			t.Errorf("%q 回落后的起点应为本月 1 号，实际 %s", key, got.From)
		}
	}
}

func TestCustomerOverviewRangeUsesUTCDayBoundary(t *testing.T) {

	now := time.Date(2026, 10, 5, 23, 0, 0, 0, time.UTC)
	got := customerOverviewRangeOf("today", "", "", now)
	if got.To != "2026-10-05" {
		t.Errorf("结束日应按 UTC 计，实际 %s", got.To)
	}
}

func TestCustomerOverviewRangeExplicitWinsOverPreset(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	got := customerOverviewRangeOf("month", "2026-10-01", "2026-10-03", now)
	if got.Key != "custom" || got.From != "2026-10-01" || got.To != "2026-10-03" {
		t.Errorf("显式区间应压过档位名，实际 %+v", got)
	}
}

func TestCustomerOverviewRangeExplicitSanitises(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		fromQ, toQ string
		wantFrom   string
		wantTo     string
	}{
		{"只给起点：终点补今天", "2026-09-01", "", "2026-09-01", "2026-10-05"},
		{"只给终点：起点补今天", "", "2026-10-05", "2026-10-05", "2026-10-05"},
		{"选反了对调", "2026-10-03", "2026-10-01", "2026-10-01", "2026-10-03"},
		{"终点超今天收敛到今天", "2026-10-01", "2026-12-31", "2026-10-01", "2026-10-05"},
		{"起点在未来拉到终点", "2026-12-01", "2026-12-31", "2026-10-05", "2026-10-05"},
		{"跨度超上限收起点（366 天）", "2020-01-01", "2026-10-05", "2025-10-05", "2026-10-05"},

		{"坏的一端当没传（补今天后对调）", "not-a-date", "2026-10-03", "2026-10-03", "2026-10-05"},
	}
	for _, c := range cases {
		got := customerOverviewRangeOf("month", c.fromQ, c.toQ, now)
		if got.From != c.wantFrom || got.To != c.wantTo {
			t.Errorf("%s: got %s ~ %s, want %s ~ %s", c.name, got.From, got.To, c.wantFrom, c.wantTo)
		}
	}
}

func TestCustomerOverviewRangeBothEndsBrokenFallsBack(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	got := customerOverviewRangeOf("week", "x", "y", now)
	if got.Key != "week" || got.From != "2026-10-05" {
		t.Errorf("两端都解析不出时应回落档位名，实际 %+v", got)
	}
}

func TestCustomerRangeQueryCarriesExplicitRange(t *testing.T) {
	got := customerRangeQuery(customerOverviewRange{Key: "custom", From: "2026-10-01", To: "2026-10-03"})
	if got != "from=2026-10-01&to=2026-10-03" {
		t.Errorf("区间 query 片段应为 from=…&to=…，实际 %s", got)
	}
	if strings.Contains(got, "range=") {
		t.Errorf("不应回带档位名：%s", got)
	}
}

func TestCustomerOverviewPageDataWithoutGrowth(t *testing.T) {
	data := customerOverviewPageData(
		customerTestPageTitle,
		customerOverviewRange{Key: "month", From: "2026-10-01", To: "2026-10-05"},
		nil, customerMsg{Key: customerUnavailableLabel.key, Fallback: customerUnavailableLabel.fallback})
	if data.GrowthReady != false {
		t.Error("没拿到数据时 GrowthReady 必须是 false（模板据此渲染空态而不是一片 0）")
	}

	if data.OrderingCustomers != 0 || data.NewCustomers != 0 || data.ReturningCustomers != 0 ||
		data.Repurchasers != 0 || data.RepurchaseRateLabel != "" {
		t.Error("拿不到数据时不该有增长数据（模板靠 GrowthReady 分流，但值仍应为零）")
	}
}

func TestCustomerOverviewPageDataCarriesGrowth(t *testing.T) {
	data := customerOverviewPageData(
		customerTestPageTitle,
		customerOverviewRange{Key: "month", From: "2026-10-01", To: "2026-10-05"},
		&orderdto.CustomerGrowthResp{
			OrderingCustomers: 8, NewCustomers: 3, ReturningCustomers: 5,
			Repurchasers: 4, NewRepurchasers: 1, RepurchaseRateLabel: "50.0%",
		}, customerMsg{})
	if data.GrowthReady != true {
		t.Fatal("拿到数据时 GrowthReady 应为 true")
	}
	if data.OrderingCustomers != int64(8) {
		t.Errorf("区间下单客户透传失败：%v", data.OrderingCustomers)
	}
	if data.RepurchaseRateLabel != "50.0%" {
		t.Errorf("复购率展示串透传失败：%v", data.RepurchaseRateLabel)
	}
}

func TestCustomerOverviewPageAsksForSelectedProject(t *testing.T) {
	growth := &fakeGrowthReader{res: &orderdto.CustomerGrowthResp{OrderingCustomers: 1}}
	h := NewCustomerPageHandle(nil, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerGrowth(growth)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/overview?range=week", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if growth.got == nil {
		t.Fatal("没有向订单模块取数")
	}
	if growth.got.ProjectID == "" {
		t.Error("必须带上工程（否则订单模块拒答）")
	}
	if growth.got.From != "2026-10-05" && growth.got.From == "" {
		t.Error("区间起点没传")
	}
}

func TestCustomerOverviewPageWithoutPortShowsNotice(t *testing.T) {
	h := NewCustomerPageHandle(nil, nil, fakeProjects{items: detailProjects()})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d（端口缺席不该 500）", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, customerUnavailableLabel.fallback) {
		t.Error("端口缺席时应显示「能力未装配」的说明")
	}
	if strings.Contains(body, "stat-value") {
		t.Error("端口缺席时不应渲染 KPI 卡 —— 那会显示成 0，被读成真实统计")
	}
}

func TestCustomerOverviewPageRendersGrowth(t *testing.T) {
	growth := &fakeGrowthReader{res: &orderdto.CustomerGrowthResp{
		OrderingCustomers: 8, NewCustomers: 3, ReturningCustomers: 5,
		Repurchasers: 4, NewRepurchasers: 1, RepurchaseRateLabel: "50.0%",
	}}
	h := NewCustomerPageHandle(nil, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerGrowth(growth)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/overview?range=month", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("响应体为空 —— handler 里的模板名很可能与模板文件对不上")
	}

	for _, want := range []string{"50.0%", "区间下单客户", "新客", "回头客", "复购率",
		`name="from"`, `name="to"`, `action="/admin/customers/overview"`, "data-wb-daterange"} {
		if !strings.Contains(body, want) {
			t.Errorf("概览页缺少 %q", want)
		}
	}
}
