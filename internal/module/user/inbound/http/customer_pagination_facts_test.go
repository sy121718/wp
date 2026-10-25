package userhttp

// customer_pagination_facts_test.go — task-117（P3-B3b）：user 模块两个整页分页站点的
// **handler 级**证据。
//
// 为什么单独一层：模板层的逐字节差分（internal/templates/admin_pagination_b3b_facts_test.go）
// 证明的是「partial 两种形态渲染同字节」，它**盖不住** handler 有没有真的把事实交出去 ——
// 一个漏改的站点会继续注入旧键，差分照样全绿。这里从 HTTP 入口打到渲染结果，
// 钉住「分页条真的出现在页面上、链接里真的带着本次筛选 + limit、信息行的页码窗口真的对」。

import (
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	userdto "go_wp/internal/module/user/dto"
)

// paginationNavOf 取出页面里的分页条片段。没渲染出来时返回空串。
func paginationNavOf(body string) string {
	start := strings.Index(body, `<nav class="pagination"`)
	if start < 0 {
		return ""
	}
	rest := body[start:]
	end := strings.Index(rest, "</nav>")
	if end < 0 {
		return ""
	}
	return rest[:end+len("</nav>")]
}

// TestCustomersListPaginationFactsWiring 客户列表：total=45 / limit=20 ⇒ 三页，
// 第 2 页的链接必须带本次筛选（keyword）与 limit，信息行必须是「第 21-40 条」。
func TestCustomersListPaginationFactsWiring(t *testing.T) {
	list := &userdto.CustomerListResp{
		List:     []*userdto.CustomerResp{customerSample()},
		Total:    45,
		Counters: userdto.CustomerCounters{Total: 45, Active: 45, Verified: 45},
	}
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: list}, fakeOrderSummaryReader{},
		fakeProjects{items: []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}}})
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers?keyword=alice&page=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("客户列表页返回 %d：%s", rec.Code, rec.Body.String())
	}

	nav := paginationNavOf(rec.Body.String())
	if nav == "" {
		t.Fatal("三页数据却没渲染分页条 —— handler 很可能没有把 pagination.Page 事实交给模板")
	}
	for _, want := range []string{
		// 这一次请求的筛选条件必须原样出现在链接里（翻页不丢条件）。
		"keyword=alice",
		// limit 由事实层补上（Page.KeepLimit 默认 true，与旧链接一致）。
		"limit=20",
		// 页码窗口：三页全列（7 个槽位装得下，无省略号）。
		// 当前页渲染成 is-active 的 span（**没有 href**），其余页才是链接。
		"page=1", "page=3", `is-active">2</span>`,
		// 信息行由 partial 现算：total / start / end 三个事实都对才有这一句。
		"共 45 条，第 21-40 条",
	} {
		if !strings.Contains(nav, want) {
			t.Errorf("分页条缺少 %q：\n%s", want, nav)
		}
	}
	if strings.Contains(nav, "…") {
		t.Errorf("三页不该出现省略号窗口：\n%s", nav)
	}
}

// TestCustomerRfmPaginationFactsKeepWindowAndSegment RFM 页：链接必须保留区间（from/to）
// 与分段（segment）—— 丢了条件翻页会静默变成「全部区间 + 全部分段」。
func TestCustomerRfmPaginationFactsKeepWindowAndSegment(t *testing.T) {
	res := rfmSample()
	res.Total = 45
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil,
		fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(&fakeRfmReader{res: res})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", adminui.PageSlot(CustomerRfmPageTemplate), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/customers/rfm?range=month&segment=vip&page=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("RFM 页返回 %d：%s", rec.Code, rec.Body.String())
	}

	nav := paginationNavOf(rec.Body.String())
	if nav == "" {
		t.Fatal("三页数据却没渲染分页条 —— handler 很可能没有把 pagination.Page 事实交给模板")
	}
	for _, want := range []string{"from=", "to=", "segment=vip", "limit=20", "page=1", "page=3",
		`is-active">2</span>`, "共 45 条"} {
		if !strings.Contains(nav, want) {
			t.Errorf("分页条缺少 %q：\n%s", want, nav)
		}
	}
}
