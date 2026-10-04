package userhttp

// customer_segment_filter_test.go — 客户列表的「消费分段」筛选（P7-b3b）。
//
// 这一批最容易错的地方不是「筛得对不对」，而是**筛不出来时显示什么**：
// 分段取回来的是 nil（没筛）还是空切片（筛了，没人），在页面上是
// 「全部客户」与「零个客户」两种结果 —— 而前者看起来完全正常。
// 下面的用例把这条边界逐条钉住。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

// fakeSegmentReader 记录收到的请求并按预设返回。
type fakeSegmentReader struct {
	ids   []int64
	total int64
	err   error
	got   *orderdto.CustomerSegmentIDsReq
	call  int
}

func (f *fakeSegmentReader) CustomerSegmentIDsByRange(_ context.Context, req *orderdto.CustomerSegmentIDsReq) (*orderdto.CustomerSegmentIDsResp, error) {
	f.call++
	f.got = req
	if f.err != nil {
		return nil, f.err
	}
	return &orderdto.CustomerSegmentIDsResp{
		ProjectID: req.ProjectID, From: req.From, To: req.To, Segment: req.Segment,
		UserIDs: f.ids, Total: f.total,
	}, nil
}

func TestCustomersPageSegmentFiltersByIDList(t *testing.T) {
	users := &fakeCustomerAdmin{list: customerListSample()}
	seg := &fakeSegmentReader{ids: []int64{9201, 9203}, total: 2}
	h := NewCustomerPageHandle(users, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/customers?segment=new&segmentFrom=2026-09-01&segmentTo=2026-09-05", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if seg.got == nil {
		t.Fatal("选了分段却没向订单模块取 id")
	}
	if seg.got.Segment != "new" {
		t.Errorf("分段名 = %q，期望 new", seg.got.Segment)
	}
	if seg.got.ProjectID == "" {
		t.Error("必须带上工程（订单侧的聚合都要显式工程作用域）")
	}
	if users.lastListRe == nil {
		t.Fatal("没有查客户列表")
	}
	if users.lastListRe.UserIDs == nil {
		t.Fatal("分段 id 必须原样传给列表查询（nil 会被当成「不限制」= 全部客户）")
	}
	if len(users.lastListRe.UserIDs) != 2 || users.lastListRe.UserIDs[0] != 9201 {
		t.Errorf("列表收到的 id = %v，期望 [9201 9203]", users.lastListRe.UserIDs)
	}
	// 分页链接必须带上分段，否则翻页会静默变成「全部客户」。
	body := rec.Body.String()
	if !strings.Contains(body, "segment=new") {
		t.Error("翻页 / 计数链接里丢了 segment —— 翻到第二页会变回全部客户")
	}
}

// TestCustomersPageSegmentZeroResultStaysEmpty 分段筛出 0 个人时，列表必须是**空**的。
//
// 这是整批里最容易写错的一条：`if len(ids) > 0` 之类的一步折算会把空切片变成 nil，
// 于是「这个分段一个人都没有」显示成「全部客户」—— 用户看到一整页人，会以为筛成功了。
func TestCustomersPageSegmentZeroResultStaysEmpty(t *testing.T) {
	users := &fakeCustomerAdmin{list: customerListSample()}
	seg := &fakeSegmentReader{ids: []int64{}, total: 0}
	h := NewCustomerPageHandle(users, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?segment=new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if users.lastListRe == nil {
		t.Fatal("没有查客户列表")
	}
	if users.lastListRe.UserIDs == nil {
		t.Fatal("零个人必须是**空切片**而不是 nil —— nil 会被当成「不限制」，页面显示全部客户")
	}
	if len(users.lastListRe.UserIDs) != 0 {
		t.Errorf("id 列表应为空，实得 %v", users.lastListRe.UserIDs)
	}
}

// TestCustomersPageSegmentFailureDoesNotFallBackToListAll 取分段失败时**不查列表**。
//
// 回落成「不筛」的后果是：筛不出来时显示全部客户，而那正是用户最可能相信的结果
// （人数变多了，看起来像筛对了）。
func TestCustomersPageSegmentFailureDoesNotFallBackToListAll(t *testing.T) {
	users := &fakeCustomerAdmin{list: customerListSample()}
	seg := &fakeSegmentReader{err: context.DeadlineExceeded}
	h := NewCustomerPageHandle(users, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?segment=new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d（取数失败不该 500）", rec.Code)
	}
	if users.lastListRe != nil {
		t.Error("分段没取到就不该查列表 —— 查出来的是「全部客户」，而页面上写着「新客」")
	}
	if !strings.Contains(rec.Body.String(), customerSegmentUnavailableText) {
		t.Error("应显示「分段筛选不可用」的提示，而不是把失败吞掉")
	}
}

// TestCustomersPageSegmentWithoutPortShowsNotice 端口没接线时同理：明确说筛不了。
func TestCustomersPageSegmentWithoutPortShowsNotice(t *testing.T) {
	users := &fakeCustomerAdmin{list: customerListSample()}
	h := NewCustomerPageHandle(users, nil, fakeProjects{items: detailProjects()})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?segment=returning", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if users.lastListRe != nil {
		t.Error("端口缺席时不该查列表")
	}
	if !strings.Contains(rec.Body.String(), customerSegmentUnavailableText) {
		t.Error("应显示「分段筛选不可用」的提示")
	}
}

// TestCustomersPageSegmentDefaultsToThisMonth 只选分段不选时间时，窗口回落本月
// （与客户概览页的默认档一致）：不回落就会变成「不限时间」，那是全站累计的新客。
func TestCustomersPageSegmentDefaultsToThisMonth(t *testing.T) {
	users := &fakeCustomerAdmin{list: customerListSample()}
	seg := &fakeSegmentReader{ids: []int64{1}, total: 1}
	h := NewCustomerPageHandle(users, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?segment=new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if seg.got == nil {
		t.Fatal("没有取分段")
	}
	if len(seg.got.From) != 10 || len(seg.got.To) != 10 {
		t.Fatalf("窗口应被填成 YYYY-MM-DD，实得 %q ~ %q", seg.got.From, seg.got.To)
	}
	if seg.got.From[8:] != "01" {
		t.Errorf("默认窗口起点应是本月 1 号，实得 %s", seg.got.From)
	}
}

// TestCustomersPageWithoutSegmentDoesNotQuerySegment 不选分段时一次都不该问订单模块
// （列表页的常态是「看全部客户」，多一次跨模块查询只是白花）。
func TestCustomersPageWithoutSegmentDoesNotQuerySegment(t *testing.T) {
	users := &fakeCustomerAdmin{list: customerListSample()}
	seg := &fakeSegmentReader{}
	h := NewCustomerPageHandle(users, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if seg.call != 0 {
		t.Errorf("没选分段时不该取分段，实际调了 %d 次", seg.call)
	}
	if users.lastListRe == nil || users.lastListRe.UserIDs != nil {
		t.Error("没选分段时 UserIDs 必须是 nil（不限制），而不是空切片（零个人）")
	}
}

// TestCustomersPageMinOrdersFiltersByIDList 只给次数（没给分段）也要能筛。
//
// 「下过 ≥5 单的客户」本身就是完整的一句话，不选分段也该成立；订单模块收到的是
// 空 segment + 次数，由它按「复购」处理（同一段代码，见 order_customer_segment.go）。
func TestCustomersPageMinOrdersFiltersByIDList(t *testing.T) {
	seg := &fakeSegmentReader{ids: []int64{41, 42}}
	admin := &fakeCustomerAdmin{list: customerListSample()}
	h := NewCustomerPageHandle(admin, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?minOrders=5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if seg.got == nil {
		t.Fatal("给了次数却没有向订单模块取 id 列表")
	}
	if seg.got.MinOrders != 5 {
		t.Errorf("次数档位应为 5，实得 %d", seg.got.MinOrders)
	}
	if seg.got.Segment != "" {
		t.Errorf("没选分段时 Segment 应为空（由订单模块按复购处理），实得 %q", seg.got.Segment)
	}
	if admin.lastListRe == nil || len(admin.lastListRe.UserIDs) != 2 {
		t.Errorf("id 列表应原样传给客户列表，实得 %+v", admin.lastListRe)
	}
	// 翻页链接必须带上次数，否则翻第二页会静默变回全部客户。
	if !strings.Contains(rec.Body.String(), "minOrders=5") {
		t.Error("翻页/计数链接应保留 minOrders")
	}
}

// TestCustomersPageMinOrdersUnknownFallsBack URL 是用户可编辑的：
// 不在这几个档位里的一律回落「不限次数」，且**不该**回落到默认门槛 2
// （回落成 2 会让 URL 上写着 7 的筛选实际跑的是 2，结果看起来正常却少了一半人）。
func TestCustomersPageMinOrdersUnknownFallsBack(t *testing.T) {
	seg := &fakeSegmentReader{ids: []int64{41}}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerSegments(seg)
	engine := newCustomerTestEngine(h)

	for _, bad := range []string{"7", "0", "-3", "abc", "2.5"} {
		seg.call, seg.got = 0, nil
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?minOrders="+bad, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("minOrders=%q 不该让整页报错，实得 %d", bad, rec.Code)
		}
		if seg.call != 0 {
			t.Errorf("minOrders=%q 应回落成「不按次数筛」，实际却去查了订单模块（%+v）", bad, seg.got)
		}
	}
}
