package producthttp

import (
	"context"
	"errors"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"
)

type pricingCatalogStub struct {
	productcontract.ProductService
	scopeErr         error
	historyErr       error
	requestedProject string
	historyReq       *productdto.ListPriceAdjustmentReq
	empty            bool
}

func (s *pricingCatalogStub) ResolveCatalogScope(_ context.Context, projectID string) (*productcontract.CatalogScope, error) {
	s.requestedProject = projectID
	if s.scopeErr != nil {
		return nil, s.scopeErr
	}
	if s.empty {
		return &productcontract.CatalogScope{}, nil
	}
	return &productcontract.CatalogScope{ProjectID: "p1", Projects: []projectdto.ProjectResp{{ID: "p1", Name: "定价工程"}}}, nil
}
func (s *pricingCatalogStub) ListPricingRuleTypes(context.Context) []*productdto.PricingRuleTypeResp {
	return []*productdto.PricingRuleTypeResp{{Type: "fixed_price"}}
}
func (s *pricingCatalogStub) ListPricingRoundingOptions(context.Context) []*productdto.PricingRoundingOptionResp {
	return []*productdto.PricingRoundingOptionResp{{Value: "none"}}
}
func (s *pricingCatalogStub) ListPriceAdjustments(_ context.Context, req *productdto.ListPriceAdjustmentReq) ([]*productdto.PriceAdjustmentResp, error) {
	s.historyReq = req
	return []*productdto.PriceAdjustmentResp{}, s.historyErr
}

func pricingPageRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/product-pricing", pageview.UseRenderer(pageview.RenderPage), h.ProductPricingPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestProductPricingPageUsesBusinessObjectsAndHTMLWrites(t *testing.T) {
	service := &pricingCatalogStub{}
	rec := pricingPageRequest(t, &ProductPageHandle{products: service}, "/admin/product-pricing?project=p1")
	if rec.Code != http.StatusOK {
		t.Fatalf("页面应成功：%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"</html>", "定价工具", "统一售价", "调价留痕", "data-pricing-preview", "data-write-status", "hx-post=\"/api/product/pricing/preview\"", "hx-post=\"/api/product/pricing/apply\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q：%s", want, body)
		}
	}
	if strings.Contains(body, "action=\"/admin/product-pricing/preview\"") {
		t.Fatal("不能恢复页面POST预览")
	}
	if service.requestedProject != "p1" || service.historyReq == nil || !service.historyReq.IncludeItems || service.historyReq.ProjectID != "p1" {
		t.Fatalf("查询未使用工程范围与批量明细：%+v", service.historyReq)
	}
}

func TestProductPricingPageDoesNotHideReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service *pricingCatalogStub
	}{
		{"工程失败", &pricingCatalogStub{scopeErr: errors.New("private project db error")}},
		{"记录失败", &pricingCatalogStub{historyErr: errors.New("private history db error")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := pricingPageRequest(t, &ProductPageHandle{products: tc.service}, "/admin/product-pricing?project=p1")
			if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("失败应为500 JSON：%d %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatal("错误泄漏数据库原文")
			}
		})
	}
}

func TestProductPricingEmptyScopeDoesNotReadOtherProjects(t *testing.T) {
	service := &pricingCatalogStub{empty: true}
	rec := pricingPageRequest(t, &ProductPageHandle{products: service}, "/admin/product-pricing")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "</html>") {
		t.Fatalf("空工程目录应完整渲染：%d %s", rec.Code, rec.Body.String())
	}
	if service.historyReq != nil {
		t.Fatal("空工程不得调用可能兜底到其他工程的历史查询")
	}
}
