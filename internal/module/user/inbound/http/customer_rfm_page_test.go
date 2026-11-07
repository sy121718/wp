package userhttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

type fakeRfmReader struct {
	res *orderdto.CustomerRfmResp
	err error
	got *orderdto.CustomerRfmReq

	call int

	segRes  *orderdto.CustomerRfmSegmentIDsResp
	segGot  *orderdto.CustomerRfmSegmentIDsReq
	segErr  error
	segCall int
}

func (f *fakeRfmReader) CustomerRfmSegmentIDsByRange(_ context.Context, req *orderdto.CustomerRfmSegmentIDsReq) (*orderdto.CustomerRfmSegmentIDsResp, error) {
	f.segCall++
	f.segGot = req
	if f.segErr != nil {
		return nil, f.segErr
	}
	if f.segRes != nil {
		return f.segRes, nil
	}
	return &orderdto.CustomerRfmSegmentIDsResp{Segment: req.Segment, UserIDs: []int64{}}, nil
}

func (f *fakeRfmReader) CustomerRfmByRange(_ context.Context, req *orderdto.CustomerRfmReq) (*orderdto.CustomerRfmResp, error) {
	f.call++
	f.got = req
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func rfmSample() *orderdto.CustomerRfmResp {
	return &orderdto.CustomerRfmResp{
		ProjectID: "p1", From: "2026-10-01", To: "2026-10-05",
		Customers: 2, Vip: 1, Potential: 1, LowValue: 0, Total: 2,
		Items: []orderdto.CustomerRfmItem{
			{
				UserID: 106, LastOrderAt: "2026-10-04", RecencyDays: 1,
				Frequency: 5, MonetaryCents: 123450, MonetaryLabel: "CNY 1,234.50",
				RScore: 5, FScore: 5, MScore: 4, TotalScore: 14,

				Segment: "vip",
			},
			{
				UserID: 107, LastOrderAt: "2026-09-20", RecencyDays: 15,
				Frequency: 1, MonetaryCents: 20000, MonetaryLabel: "CNY 200.00",
				RScore: 3, FScore: 2, MScore: 2, TotalScore: 7,
				Segment: "low_value",
			},
		},
	}
}

func TestCustomerRfmPageRendersRows(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?range=month", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("响应体为空 —— handler 里的模板名很可能与模板文件对不上")
	}
	for _, want := range []string{
		"RFM 分析", "CNY 1,234.50", "2026-10-04", "高价值", "一般",
		"5 / 5 / 4", "14", "/admin/customers/detail?id=106",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("RFM 页缺少 %q", want)
		}
	}

	if n := strings.Count(body, "<th>"); n != 8 {
		t.Errorf("表头应为 8 列，实得 %d", n)
	}
}

func TestCustomerRfmPageEmptySegmentKeepsTableShape(t *testing.T) {
	empty := &orderdto.CustomerRfmResp{
		ProjectID: "p1", From: "2026-10-01", To: "2026-10-05",
		Customers: 2, Vip: 0, Potential: 1, LowValue: 1, Total: 0,
		Items: []orderdto.CustomerRfmItem{},
	}
	rfm := &fakeRfmReader{res: empty}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?segment=vip", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "<table") {
		t.Fatal("筛出 0 个人时表格仍应在（否则用户看不到自己处在哪个筛选）")
	}
	if !strings.Contains(body, `colspan="8"`) {
		t.Error("空态行的 colspan 必须等于列数（8）")
	}
	if !strings.Contains(body, "这个分段在这段时间里没有客户") {
		t.Error("空态文案缺失")
	}

	if !strings.Contains(body, ">2<") && !strings.Contains(body, "2 ") {
		t.Log("头部计数渲染形式变了，检查客户数是否仍显示 2")
	}
}

func TestCustomerRfmPageTabURLsKeepRange(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?range=year", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`href="/admin/customers/rfm?from=`,
		`&amp;segment=vip"`,
		`&amp;segment=potential"`,
		`&amp;segment=low_value"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("分段徽章链接缺少 %q", want)
		}
	}

	if strings.Contains(body, `href="/admin/customers/rfm?range=`) {
		t.Error("分段徽章链接不应回带档位名：?from=&to= 才是生效窗口的真源")
	}
}

func TestCustomerRfmPagePassesSegmentAndWindow(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?range=month&segment=vip", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if rfm.got == nil {
		t.Fatal("没有向订单模块取 RFM")
	}
	if rfm.got.Segment != "vip" {
		t.Errorf("分段 = %q，期望 vip", rfm.got.Segment)
	}
	if rfm.got.ProjectID == "" {
		t.Error("必须带上工程（订单侧聚合都要显式工程作用域）")
	}
	if len(rfm.got.From) != 10 || len(rfm.got.To) != 10 {
		t.Errorf("区间应被解析成 YYYY-MM-DD，实得 %q ~ %q", rfm.got.From, rfm.got.To)
	}
}

func TestCustomerRfmPageUnknownSegmentFallsBack(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?segment=gold", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("手改出来的未知分段不该让整页报错，实得 %d", rec.Code)
	}
	if rfm.got == nil || rfm.got.Segment != "" {
		t.Errorf("未知分段应回落成空（全部），实得 %q", rfm.got.Segment)
	}
}

func TestCustomerRfmPageWithoutPortShowsNotice(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d（端口缺席不该 500）", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, customerRfmUnavailableLabel.fallback) {
		t.Error("端口缺席时应显示「暂时不可用」的说明")
	}

	if !strings.Contains(body, "<table") {
		t.Error("端口缺席时表头仍应保留（空态吃掉表头会让用户看不到有哪些列）")
	}
	if strings.Contains(body, "这个分段在这段时间里没有客户") {
		t.Error("端口缺席不等于「该分段没有客户」—— 那是两种不同的空，文案不能共用")
	}
}

func TestCustomerRfmPageWithoutProjectsShowsEmpty(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if rfm.call != 0 {
		t.Error("没有工程时不该去查订单模块（那边会直接拒）")
	}
	if !strings.Contains(rec.Body.String(), "这段时间里没有可分析的客户") {
		t.Error("没有工程时应显示空态说明")
	}
}
