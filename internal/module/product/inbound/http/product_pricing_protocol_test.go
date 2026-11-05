package producthttp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"
)

type pricingProtocolStub struct {
	productcontract.ProductService
	preview *productdto.PricingPreviewReq
	apply   *productdto.PricingApplyReq
	fail    bool
}

func (s *pricingProtocolStub) PreviewPricing(_ context.Context, req *productdto.PricingPreviewReq) (*productdto.PricingPreviewResp, error) {
	s.preview = req
	if s.fail {
		return nil, productenums.Err(productenums.ErrPricingRuleParamsInvalid)
	}
	return &productdto.PricingPreviewResp{TargetCount: 1, Lines: []*productdto.PricingLineResp{{ProductName: "样例", OldPrice: 10, NewPrice: 12.5, Diff: 2.5}}}, nil
}
func (s *pricingProtocolStub) ApplyPricing(_ context.Context, req *productdto.PricingApplyReq) (*productdto.PricingApplyResp, error) {
	s.apply = req
	return &productdto.PricingApplyResp{AdjustmentID: "batch-1", ChangedCount: 1}, nil
}

func TestPricingPreviewAndApplyShareFormDataContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &pricingProtocolStub{}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Set("user_id", int64(9)); c.Next() })
	handle := NewProductHandle(service)
	engine.POST("/api/product/pricing/preview", pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.PreviewPricing)
	engine.POST("/api/product/pricing/apply", pageview.HTMLFragment, handle.ApplyPricing)
	for _, action := range []string{"preview", "apply"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for key, value := range map[string]string{"projectId": "p1", "ruleType": "fixed_price", "amount": "12.5", "scope": "product", "targetId": "product-1", "rounding": "none", "note": "备注", "operatorId": "forged"} {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/product/pricing/"+action, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s需返回HTML：%d %s", action, rec.Code, rec.Body.String())
		}
		if action == "preview" && !strings.Contains(rec.Body.String(), "data-pricing-preview-result") {
			t.Fatal("预览结果应由 Jet 片段输出")
		}
		if action == "apply" && !strings.Contains(rec.Body.String(), `role="status"`) {
			t.Fatal("应用成功应返回统一成功片段")
		}
	}
	if service.preview == nil || service.apply == nil {
		t.Fatal("请求未送到业务服务")
	}
	if !bytes.Equal(service.preview.RuleParams, service.apply.RuleParams) || string(service.apply.RuleParams) != "{\"amount\":12.5}" {
		t.Fatalf("预览与应用参数不一致：%s / %s", service.preview.RuleParams, service.apply.RuleParams)
	}
	if service.apply.Note != "备注" || service.apply.OperatorID != "9" {
		t.Fatalf("表单备注或可信操作人错误：%+v", service.apply)
	}
}

func TestPricingFormParamsAndFailure(t *testing.T) {
	service := &pricingProtocolStub{}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := NewProductHandle(service)
	engine.POST("/preview", pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.PreviewPricing)
	for _, fail := range []bool{false, true} {
		service.fail = fail
		values := url.Values{"projectId": {"p1"}, "ruleType": {"fixed_price"}, "amount": {"12.5"}, "scope": {"product"}, "targetId": {"p2"}}
		req := httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if fail {
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "data-ui-error") || strings.Contains(rec.Body.String(), productenums.ErrPricingRuleParamsInvalid) {
				t.Fatalf("失败需400与可读文案：%d %s", rec.Code, rec.Body.String())
			}
		} else if rec.Code != http.StatusOK || string(service.preview.RuleParams) != "{\"amount\":12.5}" {
			t.Fatalf("表单参数丢失：%d %s", rec.Code, rec.Body.String())
		}
	}
}
