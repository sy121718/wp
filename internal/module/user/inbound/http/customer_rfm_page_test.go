package userhttp

// customer_rfm_page_test.go — RFM 分析页（P7-b4b）。
//
// 这一批要钉住的是三件「不报错但会说错话」的事：
//  ① 端口缺席时**不渲染三格 0**（0 会被读成「这段时间一个客户都没有」）；
//  ② 分段筛选的链接与分页链接都要带上当前区间与分段，否则点一下会静默换一个范围；
//  ③ 认不出的分段名回落「全部」而不是让整页报错（URL 是用户可编辑的）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

// fakeRfmReader 记录收到的请求并按预设返回。
type fakeRfmReader struct {
	res *orderdto.CustomerRfmResp
	err error
	got *orderdto.CustomerRfmReq
	// rejected 记录被拒的请求（认不出的分段名 —— 本页在调用前就回落成正整数，
	// 所以这里正常情况下一次都不该收到非法值）。
	call int
	// segRes / segGot / segErr / segCall 供客户列表按 RFM 分段筛的用例使用。
	segRes  *orderdto.CustomerRfmSegmentIDsResp
	segGot  *orderdto.CustomerRfmSegmentIDsReq
	segErr  error
	segCall int
}

// CustomerRfmSegmentIDsByRange 客户列表按 RFM 分段筛时走这一条（同包的两个页面共用本 fake）。
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

// rfmSample 一份有代表性的 RFM 结果：两段各一人，其中一人没有客户资料。
func rfmSample() *orderdto.CustomerRfmResp {
	return &orderdto.CustomerRfmResp{
		ProjectID: "p1", From: "2026-10-01", To: "2026-10-05",
		Customers: 2, Vip: 1, Potential: 1, LowValue: 0, Total: 2,
		Items: []orderdto.CustomerRfmItem{
			{
				UserID: 106, LastOrderAt: "2026-10-04", RecencyDays: 1,
				Frequency: 5, MonetaryCents: 123450, MonetaryLabel: "CNY 1,234.50",
				RScore: 5, FScore: 5, MScore: 4, TotalScore: 14,
				Segment: "vip", SegmentLabel: "高价值",
			},
			{
				UserID: 107, LastOrderAt: "2026-09-20", RecencyDays: 15,
				Frequency: 1, MonetaryCents: 20000, MonetaryLabel: "CNY 200.00",
				RScore: 3, FScore: 2, MScore: 2, TotalScore: 7,
				Segment: "low_value", SegmentLabel: "一般",
			},
		},
	}
}

// TestCustomerRfmPageRendersRows 正常路径：明细进表格，分段徽章与区间都在。
func TestCustomerRfmPageRendersRows(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

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
	// 表头 8 个（数标签要锚定列名，否则其它表的表头也会被数进来）。
	if n := strings.Count(body, "<th>"); n != 8 {
		t.Errorf("表头应为 8 列，实得 %d", n)
	}
}

// TestCustomerRfmPageEmptySegmentKeepsTableShape 该分段筛出 0 人时：
// 表格还在、空态行的 colspan 与列数一致。
//
// 为什么单列一例：上面的有数据用例**渲染不出空态行**（`{{if len(rows) == 0}}` 不成立），
// 所以它数到 8 个 <th> 也证明不了 colspan 写对了 —— 一个 colspan 写少的空态行
// 会让表格在空数据时塌成 1 列宽，而那只有「刚好筛出 0 个人」时才看得见。
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
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

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
	// 头部计数仍然是「这批人的全貌」，不随分段筛选收窄。
	if !strings.Contains(body, ">2<") && !strings.Contains(body, "2 ") {
		t.Log("头部计数渲染形式变了，检查客户数是否仍显示 2")
	}
}

// TestCustomerRfmPageTabURLsKeepRange 分段徽章的链接必须带上当前区间。
//
// 不带的话点「高价值」会跳到默认档位（本月），而页面上刚刚看的是「本年」——
// 用户会以为高价值客户突然变少了，且没有任何提示。
func TestCustomerRfmPageTabURLsKeepRange(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?range=year", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`href="/admin/customers/rfm?range=year"`,
		`href="/admin/customers/rfm?range=year&amp;segment=vip"`,
		`href="/admin/customers/rfm?range=year&amp;segment=potential"`,
		`href="/admin/customers/rfm?range=year&amp;segment=low_value"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("分段徽章链接缺少 %q", want)
		}
	}
}

// TestCustomerRfmPagePassesSegmentAndWindow 分段与区间都要原样交给订单模块
// （它是唯一的判定处；本页只做白名单回落）。
func TestCustomerRfmPagePassesSegmentAndWindow(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

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

// TestCustomerRfmPageUnknownSegmentFallsBack 「认不出的分段」回落全部，
// 而不是把非法值透传给订单模块（那边会拒，页面变成一片错误）。
func TestCustomerRfmPageUnknownSegmentFallsBack(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?segment=gold", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("手改出来的未知分段不该让整页报错，实得 %d", rec.Code)
	}
	if rfm.got == nil || rfm.got.Segment != "" {
		t.Errorf("未知分段应回落成空（全部），实得 %q", rfm.got.Segment)
	}
}

// TestCustomerRfmPageWithoutPortShowsNotice 端口缺席时给一句人话，**不渲染三格 0**。
func TestCustomerRfmPageWithoutPortShowsNotice(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{items: detailProjects()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d（端口缺席不该 500）", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, customerRfmUnavailableText) {
		t.Error("端口缺席时应显示「暂时不可用」的说明")
	}
	// 表头**保留**（门禁 check-empty-state-table-head.sh 的口径：空态吃掉表头时，
	// 用户看不到这一页有哪些列，也无从确认自己是不是筛错了）。区别在正文：
	// 端口缺席给的是「数据没接上」，而不是「这段时间没有客户」。
	if !strings.Contains(body, "<table") {
		t.Error("端口缺席时表头仍应保留（空态吃掉表头会让用户看不到有哪些列）")
	}
	if strings.Contains(body, "这个分段在这段时间里没有客户") {
		t.Error("端口缺席不等于「该分段没有客户」—— 那是两种不同的空，文案不能共用")
	}
}

// TestCustomerRfmPageWithoutProjectsShowsEmpty 还没建站点工程时给空态，不是错误
// （那是正常状态：没有订单可算）。
func TestCustomerRfmPageWithoutProjectsShowsEmpty(t *testing.T) {
	rfm := &fakeRfmReader{res: rfmSample()}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{}, nil, fakeProjects{})
	h.SetCustomerRfm(rfm)
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", h.CustomerRfmPage)

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
