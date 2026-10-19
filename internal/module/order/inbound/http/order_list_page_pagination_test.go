package orderhttp

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/pagination"
)

type orderPaginationService struct {
	ordercontract.OrderService
	total                             int64
	err                               error
	project, status, keyword, payment string
	orderID                           uint64
	offset, limit, calls              int
}

func (s *orderPaginationService) ListOrders(_ context.Context, req *orderdto.ListOrderReq) (*orderdto.OrderListResp, error) {
	s.project, s.status, s.keyword, s.payment = req.ProjectID, req.Status, req.Keyword, req.PaymentMethod
	s.offset, s.limit = req.Offset, req.Limit
	s.calls++
	return &orderdto.OrderListResp{Total: s.total}, s.err
}

func (s *orderPaginationService) ListCoupons(_ context.Context, req *orderdto.CouponListReq) (*orderdto.CouponListResp, error) {
	s.project, s.status, s.keyword = req.ProjectID, req.Status, req.Keyword
	s.offset, s.limit = req.Offset, req.Limit
	s.calls++
	return &orderdto.CouponListResp{Total: s.total}, s.err
}

func (s *orderPaginationService) ListReturns(_ context.Context, req *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error) {
	s.project, s.status, s.keyword, s.orderID = req.ProjectID, req.Status, req.Keyword, req.OrderID
	s.offset, s.limit = req.Offset, req.Limit
	s.calls++
	return &orderdto.ReturnListResp{Total: s.total}, s.err
}

type orderPaginationProjects struct {
	projectcontract.ProjectService
	err error
}

func (s orderPaginationProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return []projectcontract.ProjectResp{{ID: "p-1", Name: "Demo"}}, s.err
}

// Capture the actual handler data and render the production partial from it.
type orderPaginationRender struct {
	delegate ginrender.HTMLRender
	data     gin.H
	name     string
}

func (r *orderPaginationRender) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data.(gin.H)
	return r.delegate.Instance("admin/partials/pagination.html", data)
}

func orderPaginationPage(t *testing.T, kind, query string, svc *orderPaginationService, projects orderPaginationProjects) (*orderPaginationRender, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	renderer := &orderPaginationRender{delegate: templates.NewJetHTMLRender("", false)}
	engine.HTMLRender = renderer
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/"+kind+query, nil)
	switch kind {
	case "orders":
		NewOrderPageHandle(svc, projects, nil).OrdersPage(c)
	case "coupons":
		NewCouponPageHandle(svc, projects).CouponsPage(c)
	case "returns":
		NewReturnPageHandle(svc, projects, nil, nil).ReturnsPage(c)
	default:
		t.Fatalf("unknown page %q", kind)
	}
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if renderer.name != "admin/order/"+kind+".html" {
		t.Fatalf("template = %q", renderer.name)
	}
	return renderer, rec.Body.String()
}

func TestOrderListPagesPaginationBaseline(t *testing.T) {
	for _, kind := range []string{"orders", "coupons", "returns"} {
		t.Run(kind, func(t *testing.T) {
			svc := &orderPaginationService{total: 45}
			query := "?project=%20p-1%20&status=%20pending%20&keyword=%20A%2B%26%20&page=2&limit=20&paymentMethod=%20paypal%20&orderId=42"
			if kind == "orders" {
				query = strings.ReplaceAll(query, "&orderId=42", "")
			}
			renderer, body := orderPaginationPage(t, kind, query, svc, orderPaginationProjects{})
			if svc.project != "p-1" || svc.status != "pending" || svc.keyword != "A+&" || svc.offset != 20 || svc.limit != 20 || svc.calls != 1 {
				t.Fatalf("request changed: %+v", svc)
			}
			if kind == "orders" && svc.payment != "paypal" {
				t.Fatalf("payment = %q", svc.payment)
			}
			if kind == "returns" && svc.orderID != 42 {
				t.Fatalf("orderID = %d", svc.orderID)
			}
			q := url.Values{"keyword": {"A+&"}, "project": {"p-1"}, "status": {"pending"}}
			if kind == "orders" {
				q.Set("paymentMethod", "paypal")
			}
			base := "/admin/" + kind + "?" + q.Encode()
			pg, ok := renderer.data["Pagination"].(pagination.Page)
			if !ok {
				t.Fatalf("Pagination = %T, want pagination.Page", renderer.data["Pagination"])
			}
			if pg.Total != 45 || pg.Page != 2 || pg.Limit != 20 || pg.Base != base {
				t.Fatalf("pagination facts = %+v", pg)
			}
			for _, key := range []string{"PaginationInfo", "PaginationLinks"} {
				if _, exists := renderer.data[key]; exists {
					t.Errorf("legacy key %s remains", key)
				}
			}
			for _, want := range []string{
				`<div class="pagination-info">共 45 条，第 21-40 条</div>`,
				`<span class="pagination-btn is-active">2</span>`,
				`<a class="pagination-btn" href="` + html.EscapeString(base+"&page=1&limit=20") + `">上一页</a>`,
				`<a class="pagination-btn" href="` + html.EscapeString(base+"&page=3&limit=20") + `">下一页</a>`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %s in %s", want, body)
				}
			}
			if strings.Contains(body, "orderId=") || strings.Contains(body, "returnId=") || strings.Contains(body, "couponId=") {
				t.Errorf("expanded IDs leaked into pagination: %s", body)
			}
			if renderer.data["LoadErr"] != "" {
				t.Fatalf("LoadErr = %v", renderer.data["LoadErr"])
			}
		})
	}
}

func TestOrderListPagesPaginationWindowAndEmpty(t *testing.T) {
	for _, kind := range []string{"orders", "coupons", "returns"} {
		for _, tc := range []struct {
			name, query         string
			total               int64
			page, limit         int
			listErr, projectErr error
		}{
			{name: "empty", query: "", page: 1, limit: 20},
			{name: "single", query: "", total: 5, page: 1, limit: 20},
			{name: "offset", query: "?offset=40&limit=20", total: 5, page: 3, limit: 20},
			{name: "page wins", query: "?page=2&offset=40&limit=20", total: 5, page: 2, limit: 20},
			{name: "limit cap", query: "?page=2&limit=999", total: 5, page: 2, limit: 100},
			{name: "invalid window", query: "?page=-2&limit=-1", total: 5, page: 1, limit: 20},
			{name: "list failure", query: "", page: 1, limit: 20, listErr: errors.New("private list detail")},
			{name: "project failure", query: "?project=p-1", page: 1, limit: 20, projectErr: errors.New("private project detail")},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				svc := &orderPaginationService{total: tc.total, err: tc.listErr}
				renderer, body := orderPaginationPage(t, kind, tc.query, svc, orderPaginationProjects{err: tc.projectErr})
				if strings.Contains(body, "<nav") {
					t.Fatalf("unexpected pagination: %s", body)
				}
				pg, ok := renderer.data["Pagination"].(pagination.Page)
				wantTotal, wantPage := tc.total, tc.page
				if tc.listErr != nil || tc.projectErr != nil {
					wantTotal = 0
				}
				if wantTotal > 0 {
					wantPage = 1
				}
				pager := templates.PagerOf(pg)
				if !ok || pager.Total != wantTotal || pager.Page != wantPage || pager.Limit != tc.limit {
					t.Fatalf("empty/single pagination facts = %+v（渲染值 %+v）", pg, pager)
				}
				if renderer.data["Page"] != tc.page || renderer.data["Limit"] != tc.limit {
					t.Fatalf("window = %v/%v", renderer.data["Page"], renderer.data["Limit"])
				}
				if tc.projectErr != nil {
					if svc.calls != 0 || renderer.data["SelectedProject"] != "" {
						t.Fatalf("queried after project failure: %+v", svc)
					}
				} else if svc.calls != 1 || svc.offset != (tc.page-1)*tc.limit || svc.limit != tc.limit {
					t.Fatalf("request window = %+v", svc)
				}
				if tc.listErr != nil || tc.projectErr != nil {
					msg, _ := renderer.data["LoadErr"].(string)
					if msg == "" || strings.Contains(msg, "private") {
						t.Fatalf("error outlet = %q", msg)
					}
				}
			})
		}
	}
}
