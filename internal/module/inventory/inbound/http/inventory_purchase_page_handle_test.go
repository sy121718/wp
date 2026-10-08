package inventoryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
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

type purchaseCreateCatalog struct{ productcontract.ProductService }

func (purchaseCreateCatalog) List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) {
	return []*productdto.ProductResp{{ID: "product-1"}, {ID: "product-2"}}, nil
}

func (purchaseCreateCatalog) Get(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	return &productdto.ProductResp{ID: req.ID, Name: req.ID, Variants: []*productdto.VariantResp{{ID: strings.Replace(req.ID, "product", "variant", 1), SKUCode: fmt.Sprintf("SKU%s", strings.TrimPrefix(req.ID, "product-"))}}}, nil
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
	for _, tc := range []struct {
		name, hx string
		status   int
	}{
		{"htmx", "true", http.StatusOK},
		{"native", "", http.StatusOK},
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
			if tc.hx == "" {
				// 原生失败：渲染失败提示页（取代原先的 302 + ?err=），内部错误只进日志。
				body := rec.Body.String()
				if !strings.Contains(body, `data-jump-state="err"`) || !strings.Contains(body, "系统内部错误") {
					t.Errorf("原生失败应渲染失败提示页，实际 %s", body)
				}
				if strings.Contains(body, "duplicate purchase code") {
					t.Error("内部错误原文不应进入提示页")
				}
				return
			}
			out := rec.Body.String()
			for _, want := range []string{
				`data-purchase-create-host`, `role="alert"`, `hx-target="closest [data-purchase-create-host]"`,
				`name="code" value="  PO-001  "`, `name="remark" value="  &lt;mark&gt;待确认&lt;/mark&gt;  "`,
				`value="source-1" selected`, `value="warehouse-1" selected`,
				`value="variant-1|product-1|SKU1" selected`, `value="variant-2|product-2|SKU2" selected`,
				`name="lineQuantity" value="2"`, `name="lineQuantity" value=""`, `name="lineQuantity" value="5"`,
				`name="lineUnitPrice" value="1.20"`, `name="lineUnitPrice" value=""`, `name="lineUnitPrice" value="3.40"`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("片段缺少 %q", want)
				}
			}
			for _, field := range []string{"lineQuantity", "lineUnitPrice"} {
				got := regexp.MustCompile(`name="`+field+`" value="([^"]*)"`).FindAllStringSubmatch(out, -1)
				want := values[field]
				if len(got) != len(want) {
					t.Errorf("%s 回填行数 = %d，want %d", field, len(got), len(want))
					continue
				}
				for i := range got {
					if got[i][1] != want[i] {
						t.Errorf("%s 第 %d 行 = %q，want %q", field, i, got[i][1], want[i])
					}
				}
			}
			if strings.Index(out, `role="alert"`) > strings.Index(out, `<form method="post"`) {
				t.Error("错误槽应在表单之前")
			}
			selectedSKU := regexp.MustCompile(`name="lineSku"[^>]*>(?s:.*?)</select>`).FindAllString(out, -1)
			if len(selectedSKU) != 3 || !strings.Contains(selectedSKU[0], `value="variant-1|product-1|SKU1" selected`) || strings.Contains(selectedSKU[1], ` selected`) || !strings.Contains(selectedSKU[2], `value="variant-2|product-2|SKU2" selected`) {
				t.Errorf("SKU 选择状态未按三行次序还原：%v", selectedSKU)
			}
			if strings.Contains(out, "duplicate purchase code") || strings.Contains(out, "<mark>") {
				t.Error("内部错误与未转义的用户输入不应进入片段")
			}
		})
	}
}

func TestInventoryPurchaseCreateSuccessRedirect(t *testing.T) {
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
				t.Errorf("状态码 = %d，want 200", rec.Code)
			}
			if tc.hx != "" {
				if got := rec.Header().Get("HX-Redirect"); got != inventoryPurchasesPath {
					t.Errorf("htmx 成功跳转 = %q，want %q", got, inventoryPurchasesPath)
				}
				if rec.Header().Get("Location") != "" {
					t.Error("htmx 成功不应使用 Location")
				}
				return
			}
			// 原生成功：渲染成功提示页（取代原先的 302 + ?ok=），链接回列表页。
			body := rec.Body.String()
			if !strings.Contains(body, `data-jump-state="ok"`) || !strings.Contains(body, `href="`+inventoryPurchasesPath+`"`) {
				t.Errorf("原生成功应渲染成功提示页并带回列表链接，实际 %s", body)
			}
		})
	}
}

func TestInventoryPurchaseCreateFieldsMatchTemplate(t *testing.T) {
	raw, err := os.ReadFile("../../../../templates/admin/inventory/inventory_purchase_create_form.html")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`\.FormEcho(?:Multi)?\.([A-Za-z][A-Za-z0-9_]*)`).FindAllStringSubmatch(string(raw), -1) {
		seen[match[1]] = true
	}
	for _, field := range []string{"lineSku", "lineQuantity", "lineUnitPrice"} {
		if !strings.Contains(string(raw), `name="`+field+`"`) {
			t.Errorf("采购行字段 %s 没有对应表单控件", field)
		}
	}
	for _, field := range []string{"SKU", "Quantity", "UnitPrice"} {
		if !strings.Contains(string(raw), "row."+field) {
			t.Errorf("采购行字段 %s 未读取提交回填值", field)
		}
	}
	if len(seen) == 0 {
		t.Fatal("模板中没有回填字段，字段清单检查空转")
	}
	for _, field := range purchaseCreateFormFields {
		if !seen[field] {
			t.Errorf("字段 %s 在清单中但模板未读取", field)
		}
		delete(seen, field)
	}
	for field := range seen {
		t.Errorf("模板读取 %s 但字段清单遗漏", field)
	}
}
