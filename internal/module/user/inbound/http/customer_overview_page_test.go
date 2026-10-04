package userhttp

// customer_overview_page_test.go — 客户概览页（P7-b1）。
//
// 这里钉住三件事：① 时间档位换算（含认不出的名字回落默认，以及 UTC 日界）；
// ② 取数端口缺席时页面**不显示一片 0**（0 会被读成「这段时间一个客户都没来」）；
// ③ 模板真能渲染（键名与模板里取的对得上）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/templates"
)

// fakeGrowthReader 记录收到的请求（含工程与区间），并按预设返回。
type fakeGrowthReader struct {
	res *orderdto.CustomerGrowthResp
	err error
	got *orderdto.CustomerGrowthReq
}

func (f *fakeGrowthReader) CustomerGrowthByRange(_ context.Context, req *orderdto.CustomerGrowthReq) (*orderdto.CustomerGrowthResp, error) {
	f.got = req
	return f.res, f.err
}

// —— 区间换算 ——

func TestCustomerOverviewRangeOfPresets(t *testing.T) {
	// 2026-10-05 是周一；UTC 12:00 保证不会因时区偏移跨到前一天。
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
		got := customerOverviewRangeOf(c.key, now)
		if got.Key != c.wantKey || got.From != c.wantFrom || got.To != c.wantTo {
			t.Errorf("%s: got %+v, want from=%s to=%s key=%s", c.key, got, c.wantFrom, c.wantTo, c.wantKey)
		}
	}
}

// TestCustomerOverviewRangeWeekStartsMonday 周三取本周，起点必须是周一而不是周日。
// 起点差一天不会报错，只会让「本周新客」与订单页的「本周」对不上。
func TestCustomerOverviewRangeWeekStartsMonday(t *testing.T) {
	wed := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC) // 周三
	got := customerOverviewRangeOf("week", wed)
	if got.From != "2026-10-05" {
		t.Errorf("周三的「本周」应回溯到周一 2026-10-05，实际 %s", got.From)
	}
}

// TestCustomerOverviewRangeUnknownFallsBack 认不出的档位回落默认（不报错、不空白）。
func TestCustomerOverviewRangeUnknownFallsBack(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, key := range []string{"", "custom", "last-year", "<script>"} {
		got := customerOverviewRangeOf(key, now)
		if got.Key != customerOverviewDefaultPreset {
			t.Errorf("%q 应回落 %s，实际 %s", key, customerOverviewDefaultPreset, got.Key)
		}
		if got.From != "2026-10-01" {
			t.Errorf("%q 回落后的起点应为本月 1 号，实际 %s", key, got.From)
		}
	}
}

// TestCustomerOverviewRangeUsesUTCDayBoundary 「结束日」是 UTC 当天。
// 用本地时区的话，跨零点前后这一页与订单聚合会错开一个时区。
func TestCustomerOverviewRangeUsesUTCDayBoundary(t *testing.T) {
	// 本地时间比 UTC 早 8 小时时，UTC 仍是 10-05 的 23:00。
	now := time.Date(2026, 10, 5, 23, 0, 0, 0, time.UTC)
	got := customerOverviewRangeOf("today", now)
	if got.To != "2026-10-05" {
		t.Errorf("结束日应按 UTC 计，实际 %s", got.To)
	}
}

// —— 预设链接 ——

func TestCustomerOverviewPresetLinksMarksActiveAndBuildsPath(t *testing.T) {
	links := customerOverviewPresetLinks("week")
	if len(links) != len(customerOverviewPresets) {
		t.Fatalf("预设链接数 %d，期望 %d", len(links), len(customerOverviewPresets))
	}
	active := 0
	for _, l := range links {
		key, _ := l["Key"].(string)
		if want := customerOverviewPath + "?range=" + key; l["URL"] != want {
			t.Errorf("%s 的 URL 为 %v，期望 %s", key, l["URL"], want)
		}
		// 词条 key 必须在服务端拼全（模板不拼字符串）。
		if want := "admin.dashboard.range." + key; l["LabelKey"] != want {
			t.Errorf("%s 的 LabelKey 为 %v，期望 %s", key, l["LabelKey"], want)
		}
		if l["Active"] == true {
			active++
			if key != "week" {
				t.Errorf("选中的应是 week，实际 %s", key)
			}
		}
	}
	if active != 1 {
		t.Errorf("应恰好有一个选中项，实际 %d", active)
	}
}

// —— 组装数据 ——

func TestCustomerOverviewPageDataWithoutGrowth(t *testing.T) {
	data := customerOverviewPageData(
		templates.TranslateFunc("zh-CN"),
		customerOverviewRange{Key: "month", From: "2026-10-01", To: "2026-10-05"},
		nil, nil, "客户管理能力没有接进来")
	if data["GrowthReady"] != false {
		t.Error("没拿到数据时 GrowthReady 必须是 false（模板据此渲染空态而不是一片 0）")
	}
	for _, k := range []string{"OrderingCustomers", "NewCustomers", "ReturningCustomers", "Repurchasers", "RepurchaseRateLabel"} {
		if _, ok := data[k]; ok {
			t.Errorf("拿不到数据时不应设 %s —— 设了就会渲染成 0", k)
		}
	}
}

func TestCustomerOverviewPageDataCarriesGrowth(t *testing.T) {
	data := customerOverviewPageData(
		templates.TranslateFunc("zh-CN"),
		customerOverviewRange{Key: "month", From: "2026-10-01", To: "2026-10-05"},
		nil,
		&orderdto.CustomerGrowthResp{
			OrderingCustomers: 8, NewCustomers: 3, ReturningCustomers: 5,
			Repurchasers: 4, NewRepurchasers: 1, RepurchaseRateLabel: "50.0%",
		}, "")
	if data["GrowthReady"] != true {
		t.Fatal("拿到数据时 GrowthReady 应为 true")
	}
	if data["OrderingCustomers"] != int64(8) && data["OrderingCustomers"] != 8 {
		t.Errorf("区间下单客户透传失败：%v", data["OrderingCustomers"])
	}
	if data["RepurchaseRateLabel"] != "50.0%" {
		t.Errorf("复购率展示串透传失败：%v", data["RepurchaseRateLabel"])
	}
}

// —— handler ——

// TestCustomerOverviewPageAsksForSelectedProject 工程维度：?project= 优先，没有就用第一个。
// 不传工程会让订单模块直接拒（ErrProjectRequired），页面显示的是一句取数失败。
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

// TestCustomerOverviewPageWithoutPortShowsNotice 端口缺席时给一句人话，不显示一片 0。
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

// TestCustomerOverviewPageRendersGrowth 正常路径：数字进页面，模板不中断。
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
	for _, want := range []string{"50.0%", "区间下单客户", "新客", "回头客", "复购率", "/admin/customers/overview?range=week"} {
		if !strings.Contains(body, want) {
			t.Errorf("概览页缺少 %q", want)
		}
	}
}
