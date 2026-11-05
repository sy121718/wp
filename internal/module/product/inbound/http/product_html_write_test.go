package producthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// productHTMLWriteStub 只核对传输与出口，事务和归属由真实 PG feature 验证。
type productHTMLWriteStub struct {
	productcontract.ProductService
	created *productdto.CreateReq
	updated *productdto.UpdateReq
	deleted *productdto.DeleteReq
	bulk    *productdto.BulkDeleteReq
	err     error
}

func (s *productHTMLWriteStub) Create(_ context.Context, req *productdto.CreateReq) (*productdto.ProductResp, error) {
	s.created = req
	return &productdto.ProductResp{ID: "created-id", ProjectID: req.ProjectID, Name: req.Name}, s.err
}

func (s *productHTMLWriteStub) Update(_ context.Context, req *productdto.UpdateReq) (*productdto.ProductResp, error) {
	s.updated = req
	return &productdto.ProductResp{ID: req.ID, ProjectID: req.ProjectID}, s.err
}

func (s *productHTMLWriteStub) Delete(_ context.Context, req *productdto.DeleteReq) error {
	s.deleted = req
	return s.err
}

func (s *productHTMLWriteStub) BulkDeleteProducts(_ context.Context, req *productdto.BulkDeleteReq) (*productdto.BulkResultResp, error) {
	s.bulk = req
	return &productdto.BulkResultResp{}, s.err
}

func productHTMLWriteEngine(svc *productHTMLWriteStub, middleware ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(view.FailureMiddleware(adminui.RenderFailure))
	engine.Use(view.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Set("username", "trusted-operator")
		c.Next()
	})
	engine.Use(middleware...)
	registerProductCoreAPI(mw.NewRouteGroup(engine.Group("/api/product"), mw.RouteProtected), NewProductHandle(svc))
	return engine
}

func productHTMLForm(engine *gin.Engine, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// 客户端头不能改变路由声明的输出协议。
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func assertProductHTMLWrite(t *testing.T, rec *httptest.ResponseRecorder, status int, marker string) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, marker) {
		t.Fatalf("HTML 出口不符：%d %s %s", rec.Code, rec.Header().Get("Content-Type"), body)
	}
	for _, forbidden := range []string{"<!DOCTYPE", "<html", "<body", "SQLSTATE", "private-error"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("局部响应含整页或内部信息 %q：%s", forbidden, body)
		}
	}
	for _, header := range []string{"Location", "HX-Redirect", "HX-Location", "HX-Refresh"} {
		if rec.Header().Get(header) != "" {
			t.Fatalf("后端不应控制导航：%s", header)
		}
	}
}

func TestProductCreateHTMLNativeForm(t *testing.T) {
	for _, price := range []string{"absent", "0", "12.50"} {
		t.Run(price, func(t *testing.T) {
			svc := &productHTMLWriteStub{}
			values := url.Values{"name": {"商品 <script>"}, "projectId": {"body-project"}, "attributeIds": {"attr-1", "attr-2"}, "warehouseIds": {"warehouse-1", "warehouse-2"}, "operatorId": {"forged"}}
			if price != "absent" {
				values.Set("defaultPrice", price)
			}
			rec := productHTMLForm(productHTMLWriteEngine(svc), "/api/product/create?name=query-name&projectId=query-project&trackQuantity=true&quantity=99", values)
			assertProductHTMLWrite(t, rec, http.StatusOK, "data-product-created-link")
			req := svc.created
			if req == nil || req.Name != values.Get("name") || req.ProjectID != "body-project" || req.OperatorID != "trusted-operator" || req.Quantity != nil || req.TrackQuantity {
				t.Fatalf("必须只绑定 body 并注入可信操作人：%+v", req)
			}
			if !reflect.DeepEqual(req.AttributeIDs, values["attributeIds"]) || !reflect.DeepEqual(req.WarehouseIDs, values["warehouseIds"]) {
				t.Fatalf("重复集合字段丢失：%+v", req)
			}
			if price == "absent" && req.DefaultPrice != nil || price == "0" && (req.DefaultPrice == nil || *req.DefaultPrice != 0) || price == "12.50" && (req.DefaultPrice == nil || *req.DefaultPrice != 12.5) {
				t.Fatalf("未设置、0 和实际金额混淆：%v", req.DefaultPrice)
			}
			if !strings.Contains(rec.Body.String(), "/admin/products/edit?project=body-project&amp;product=created-id") || strings.Contains(rec.Body.String(), "<script>") {
				t.Fatalf("Jet 应生成真实编辑链接并转义名称：%s", rec.Body.String())
			}
		})
	}
}

func TestProductUpdateHTMLStructuredJSON(t *testing.T) {
	svc := &productHTMLWriteStub{}
	body := `{"id":"body-id","projectId":"body-project","name":"商品","description":{"html":"<p>描述</p>"},"metadata":{"featured":true},"defaultPrice":0,"images":[],"imageAlts":[],"attributeIds":[],"categoryIds":[],"tagIds":[],"brandId":"","primaryCategoryId":"","operatorId":"forged"}`
	req := httptest.NewRequest(http.MethodPost, "/api/product/update?id=query-id&projectId=query-project", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	productHTMLWriteEngine(svc).ServeHTTP(rec, req)
	assertProductHTMLWrite(t, rec, http.StatusOK, `role="status"`)
	got := svc.updated
	if got == nil || got.ID != "body-id" || got.ProjectID != "body-project" || got.OperatorID != "trusted-operator" || got.DefaultPrice == nil || *got.DefaultPrice != 0 {
		t.Fatalf("复杂编辑输入或可信操作人丢失：%+v", got)
	}
	if string(got.Description) != `{"html":"<p>描述</p>"}` || string(got.Metadata) != `{"featured":true}` || got.Images == nil || got.AttributeIDs == nil || got.CategoryIDs == nil || got.TagIDs == nil || got.BrandID == nil || *got.BrandID != "" || got.PrimaryCategoryID == nil || *got.PrimaryCategoryID != "" {
		t.Fatalf("复杂对象或显式清空字段丢失：%+v", got)
	}
	for _, body := range []string{`{"name":"missing-id"}`, `{"id":"x","defaultPrice":""}`, `{"id":`} {
		svc.updated = nil
		req := httptest.NewRequest(http.MethodPost, "/api/product/update", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		productHTMLWriteEngine(svc).ServeHTTP(rec, req)
		assertProductHTMLWrite(t, rec, http.StatusBadRequest, "data-ui-error")
		if svc.updated != nil {
			t.Fatal("非法 JSON 不应进入 Service")
		}
	}
}

func TestProductCreateHTMLTrackedZero(t *testing.T) {
	svc := &productHTMLWriteStub{}
	rec := productHTMLForm(productHTMLWriteEngine(svc), "/api/product/create", url.Values{"name": {"跟踪商品"}, "trackQuantity": {"1"}, "quantity": {"0"}})
	assertProductHTMLWrite(t, rec, http.StatusOK, "data-product-created-link")
	if !svc.created.TrackQuantity || svc.created.Quantity == nil || *svc.created.Quantity != 0 || len(svc.created.AttributeIDs) != 0 || len(svc.created.WarehouseIDs) != 0 {
		t.Fatalf("跟踪零库存和全不选集合语义错误：%+v", svc.created)
	}
}

func TestProductHTMLWriteRejectsInvalidBody(t *testing.T) {
	for _, tc := range []struct{ path, body, contentType string }{
		{"/api/product/create?name=query-only", "", "application/x-www-form-urlencoded"},
		{"/api/product/create", "name=bad&defaultPrice=not-a-price", "application/x-www-form-urlencoded"},
		{"/api/product/create", `{"name":"json-is-not-form"}`, "application/json"},
		{"/api/product/delete?id=query-only", "", "application/x-www-form-urlencoded"},
	} {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			svc := &productHTMLWriteStub{}
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			productHTMLWriteEngine(svc).ServeHTTP(rec, req)
			assertProductHTMLWrite(t, rec, http.StatusBadRequest, "data-ui-error")
			if svc.created != nil || svc.deleted != nil {
				t.Fatal("绑定失败不应进入 Service")
			}
		})
	}
}

func TestProductDeleteHTMLNativeForm(t *testing.T) {
	svc := &productHTMLWriteStub{}
	engine := productHTMLWriteEngine(svc)
	rec := productHTMLForm(engine, "/api/product/delete?id=wrong&projectId=wrong", url.Values{"id": {"body-id"}, "projectId": {"body-project"}, "operatorId": {"forged"}})
	assertProductHTMLWrite(t, rec, http.StatusOK, `role="status"`)
	if svc.deleted == nil || svc.deleted.ID != "body-id" || svc.deleted.ProjectID != "body-project" || svc.deleted.OperatorID != "trusted-operator" {
		t.Fatalf("删除输入错误：%+v", svc.deleted)
	}
	rec = productHTMLForm(engine, "/api/product/bulk-delete?ids=query-id", url.Values{"ids": {"body-1", "body-2"}, "projectId": {"body-project"}})
	assertProductHTMLWrite(t, rec, http.StatusOK, `role="status"`)
	if svc.bulk == nil || svc.bulk.ProjectID != "body-project" || !reflect.DeepEqual(svc.bulk.IDs, []string{"body-1", "body-2"}) {
		t.Fatalf("批量重复字段绑定错误：%+v", svc.bulk)
	}
}

func TestProductHTMLWriteErrorsAndEarlyCSRF(t *testing.T) {
	for _, path := range []string{"create", "delete", "bulk-delete"} {
		t.Run(path, func(t *testing.T) {
			svc := &productHTMLWriteStub{err: errors.New("SQLSTATE private-error")}
			values := url.Values{"name": {"商品"}, "id": {"product-1"}, "ids": {"product-1"}}
			rec := productHTMLForm(productHTMLWriteEngine(svc), "/api/product/"+path, values)
			assertProductHTMLWrite(t, rec, http.StatusInternalServerError, "data-ui-error")
			svc = &productHTMLWriteStub{}
			rec = productHTMLForm(productHTMLWriteEngine(svc, builtin.CSRFMiddleware()), "/api/product/"+path, values)
			assertProductHTMLWrite(t, rec, http.StatusForbidden, "data-ui-error")
			if svc.created != nil || svc.deleted != nil || svc.bulk != nil {
				t.Fatal("CSRF 拒绝后不应调用业务")
			}
		})
	}
}
