package inventoryhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/templates"
)

type purchaseCreateFailService struct {
	inventorycontract.InventoryService
	got *inventorydto.CreatePurchaseOrderReq
	err error
}

func (s *purchaseCreateFailService) CreatePurchaseOrder(_ context.Context, req *inventorydto.CreatePurchaseOrderReq) (*inventorydto.PurchaseOrderResp, error) {
	s.got = req
	return &inventorydto.PurchaseOrderResp{}, s.err
}

func (s *purchaseCreateFailService) ListSources(context.Context, *inventorydto.ListSourceReq) ([]*inventorydto.SourceResp, error) {
	return []*inventorydto.SourceResp{{ID: "source-1", Code: "SUP", Name: "供应商"}}, nil
}

func (s *purchaseCreateFailService) ListStocks(context.Context, *inventorydto.ListStockReq) ([]*inventorydto.StockResp, error) {
	return []*inventorydto.StockResp{}, nil
}

func (s *purchaseCreateFailService) ListWarehouses(context.Context, *inventorydto.ListWarehouseReq) ([]*inventorydto.WarehouseResp, error) {
	return []*inventorydto.WarehouseResp{{ID: "warehouse-1", Code: "WH", Name: "主仓"}}, nil
}

// purchaseCreateCatalog 只实现页面下拉需要的那个只读口（一次批量，没有 List/Get）。
type purchaseCreateCatalog struct {
	productcontract.VariantOptionsReader
}

func (purchaseCreateCatalog) ListVariantOptions(context.Context, string) ([]productcontract.VariantOption, error) {
	return []productcontract.VariantOption{
		{VariantID: "variant-1", ProductID: "product-1", SKUCode: "SKU1", ProductName: "product-1"},
		{VariantID: "variant-2", ProductID: "product-2", SKUCode: "SKU2", ProductName: "product-2"},
	}, nil
}

func purchaseCreateContext(t *testing.T, hx string, values url.Values) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/inventory/purchases/create", strings.NewReader(values.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx != "" {
		c.Request.Header.Set("HX-Request", hx)
	}
	return c, rec
}

func TestInventoryPurchaseCreateFailedSubmission(t *testing.T) {
	values := url.Values{
		"projectId": {"project-1"}, "code": {"  PO-001  "}, "sourceId": {"source-1"},
		"warehouseId": {"warehouse-1"}, "remark": {"  <mark>待确认</mark>  "},
		"lineSku":      {"variant-1|product-1|SKU1", "", "variant-2|product-2|SKU2"},
		"lineQuantity": {"2", "", "5"}, "lineUnitPrice": {"1.20", "", "3.40"},
	}
	// htmx：4xx + JSON（抽屉不 swap，输入原样保留）；原生：整页失败提示。
	for _, tc := range []struct {
		name, hx string
		status   int
	}{
		{"htmx", "true", http.StatusInternalServerError},
		{"native", "", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := purchaseCreateContext(t, tc.hx, values)
			svc := &purchaseCreateFailService{err: errors.New("duplicate purchase code")}
			(&inventoryPurchasePageHandle{inventory: svc, products: purchaseCreateCatalog{}}).InventoryPurchaseCreate(c)
			c.Writer.WriteHeaderNow()
			if rec.Code != tc.status {
				t.Fatalf("状态码 %d，want %d；正文 %s", rec.Code, tc.status, rec.Body.String())
			}
			if svc.got == nil || len(svc.got.Lines) != 2 {
				t.Fatalf("提交到服务的采购行 = %+v，want 两行（中间空行跳过）", svc.got)
			}
			body := rec.Body.String()
			if strings.Contains(body, "duplicate purchase code") || strings.Contains(body, "<mark>") {
				t.Error("内部错误与未转义的用户输入不应进入响应")
			}
			if tc.hx == "" {
				// 原生失败：同样是 JSON（不再有整页提示页），内部错误只进日志。
				if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
					t.Fatalf("原生失败 Content-Type = %q，want application/json", rec.Header().Get("Content-Type"))
				}
				if !strings.Contains(body, "系统内部错误") {
					t.Errorf("原生失败应给出归口文案，实际 %s", body)
				}
				return
			}
			// htmx 失败：JSON 一句话，不渲染抽屉片段（回填通道已删）。
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("Content-Type = %q，want application/json", rec.Header().Get("Content-Type"))
			}
			if strings.Contains(body, "data-purchase-create-host") || strings.Contains(body, "role=\"alert\"") {
				t.Fatalf("抽屉失败不该重渲片段，实际 %s", body)
			}
			if rec.Header().Get("HX-Redirect") != "" {
				t.Fatalf("抽屉失败不该整页跳转，HX-Redirect = %q", rec.Header().Get("HX-Redirect"))
			}
		})
	}
}

// TestInventoryPurchaseCreateSuccessIsJSON 成功 = 200 + JSON，两种请求形态都一样。
//
// 后端不给跳转地址（不回 302 / HX-Redirect）：导航由模板的 data-write-nav 决定，
// 页码与筛选本来就完整在地址栏里，服务端再算一遍就是第二份真相。
func TestInventoryPurchaseCreateSuccessIsJSON(t *testing.T) {
	for _, tc := range []struct {
		name, hx string
	}{
		{"htmx", "true"},
		{"native", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := purchaseCreateContext(t, tc.hx, url.Values{"projectId": {"project-1"}})
			(&inventoryPurchasePageHandle{inventory: &purchaseCreateFailService{}}).InventoryPurchaseCreate(c)
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK {
				t.Fatalf("成功状态码 = %d，want 200（%s）", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("HX-Redirect"); got != "" {
				t.Errorf("成功不该给 HX-Redirect = %q", got)
			}
			if got := rec.Header().Get("Location"); got != "" {
				t.Errorf("成功不该给 Location = %q", got)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("Content-Type = %q，want application/json", ct)
			}
		})
	}
}
