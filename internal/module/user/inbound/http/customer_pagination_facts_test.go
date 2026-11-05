package userhttp

import (
	pageview "go_wp/pkg/view"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	userdto "go_wp/internal/module/user/dto"
)

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

		"keyword=alice",

		"limit=20",

		"page=1", "page=3", `is-active">2</span>`,

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

func TestCustomerRfmPaginationFactsKeepWindowAndSegment(t *testing.T) {
	res := rfmSample()
	res.Total = 45
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil,
		fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(&fakeRfmReader{res: res})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, adminui.RenderPage), h.CustomerRfmPage)

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
