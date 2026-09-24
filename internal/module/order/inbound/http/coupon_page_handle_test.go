package orderhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
)

type couponEditProjectStub struct {
	projectcontract.ProjectService
	exists bool
	err    error
}

func (p couponEditProjectStub) Exists(_ context.Context, _ string) (bool, error) {
	return p.exists, p.err
}

func TestCouponEditFormFragment(t *testing.T) {
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name          string
		query         string
		coupon        *orderdto.CouponResp
		getErr        error
		projectExists bool
		wantStatus    int
		wantGet       bool
	}{
		{name: "valid", query: "id=7&project=p1&keyword=hello&page=2&evil=injected", coupon: &orderdto.CouponResp{ID: 7, ProjectID: "p1", Code: "SAVE20", Name: "Test", DiscountType: "fixed", DiscountValue: 100, Status: 1}, projectExists: true, wantStatus: http.StatusOK, wantGet: true},
		{name: "missing id", query: "project=p1", projectExists: true, wantStatus: http.StatusBadRequest},
		{name: "invalid id", query: "id=oops&project=p1", projectExists: true, wantStatus: http.StatusBadRequest},
		{name: "noncanonical id", query: "id=%2B7&project=p1", projectExists: true, wantStatus: http.StatusBadRequest},
		{name: "missing project", query: "id=7", wantStatus: http.StatusBadRequest},
		{name: "nonexistent project", query: "id=7&project=gone", wantStatus: http.StatusNotFound},
		{name: "nonexistent coupon", query: "id=7&project=p1", getErr: errors.New(orderenums.ErrCouponNotFound), projectExists: true, wantStatus: http.StatusNotFound, wantGet: true},
		{name: "foreign coupon", query: "id=7&project=p1", coupon: &orderdto.CouponResp{ID: 7, ProjectID: "p2", Code: "SECRET"}, projectExists: true, wantStatus: http.StatusNotFound, wantGet: true},
		{name: "wrong id returned", query: "id=7&project=p1", coupon: &orderdto.CouponResp{ID: 8, ProjectID: "p1", Code: "SECRET"}, projectExists: true, wantStatus: http.StatusNotFound, wantGet: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orders := &fakeCouponOrderService{getCoupon: tc.coupon, getErr: tc.getErr}
			h := &couponPageHandle{orders: orders, projects: couponEditProjectStub{exists: tc.projectExists}}
			engine := gin.New()
			engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
			engine.GET("/admin/coupons/edit-form", func(c *gin.Context) {
				if err := auth.SetSessionValue(c, "csrf_token", "coupon-test-csrf"); err != nil {
					t.Fatal(err)
				}
				c.Next()
			}, h.CouponEditForm)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/coupons/edit-form?"+tc.query, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if (orders.getCalls > 0) != tc.wantGet {
				t.Errorf("GetCoupon calls=%d wantGet=%v", orders.getCalls, tc.wantGet)
			}
			if rec.Code != http.StatusOK {
				if strings.Contains(rec.Body.String(), "SECRET") || strings.Contains(rec.Body.String(), "data-drawer-fragment") {
					t.Errorf("failed lookup leaked coupon fragment: %s", rec.Body.String())
				}
				return
			}
			out := strings.TrimSpace(rec.Body.String())
			if strings.Count(out, "data-drawer-fragment") != 1 || !strings.HasPrefix(out, "<div data-coupon-edit-host data-drawer-fragment>") || !strings.HasSuffix(out, "</div>") {
				t.Errorf("expected self-contained single root: %s", out)
			}
			for _, want := range []string{"<form ", `name="csrf_token" value="coupon-test-csrf"`, `name="projectId" value="p1"`, `id="coupon-7-code" value="SAVE20" readonly`, `name="returnQuery" value="`} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q", want)
				}
			}
			if !strings.Contains(out, "keyword=hello") || !strings.Contains(out, "page=2") || strings.Contains(out, "evil") {
				t.Errorf("returnQuery is not allowlisted: %s", out)
			}
			if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Errorf("content type=%q", rec.Header().Get("Content-Type"))
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("cache control=%q", rec.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestCouponEditFormRequiresUpdatePermissionMiddleware(t *testing.T) {
	h := &couponPageHandle{orders: &fakeCouponOrderService{getCoupon: &orderdto.CouponResp{ID: 7, ProjectID: "p1"}}, projects: couponEditProjectStub{exists: true}}
	engine := gin.New()
	engine.GET("/admin/coupons/edit-form", builtin.CasbinMiddlewareForPathAs("/api/order/coupon/update", http.MethodPost), h.CouponEditForm)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/coupons/edit-form?id=7&project=p1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without identity status=%d want=401", rec.Code)
	}
}

func TestCouponEditFormRouteReusesPostUpdatePermission(t *testing.T) {
	raw, err := os.ReadFile("order_router.go")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`pages\.GET\("/coupons/edit-form",\s*builtin\.CasbinMiddlewareForPathAs\("/api/order/coupon/update",\s*http\.MethodPost\),\s*couponPages\.CouponEditForm\)`)
	if !pattern.Match(raw) {
		t.Fatal("edit form GET route must enforce the real coupon update POST permission")
	}
	if permission.OrderCouponUpdate != "order:coupon_update" {
		t.Fatalf("unexpected update permission code: %s", permission.OrderCouponUpdate)
	}
}

func TestCouponEditFormReturnQueryAllowlist(t *testing.T) {
	values, err := url.ParseQuery(couponBackQuery("p1", couponFilter{Keyword: "hello"}, 2, 20, 7))
	if err != nil {
		t.Fatal(err)
	}
	for key := range values {
		if _, ok := couponBackKeys[key]; !ok {
			t.Errorf("unexpected return key %s", key)
		}
	}
}
