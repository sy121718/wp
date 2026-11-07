package producthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

type fakeProductService struct {
	productcontract.ProductService
	detail *productdto.ProductResp
	listed []*productdto.ProductResp
	cats   []*productdto.CategoryResp
	brands []*productdto.BrandResp
	err    error
}

func (f *fakeProductService) Get(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.detail == nil || req.ID != f.detail.ID {
		return nil, productenums.Err(productenums.ErrNotFound)
	}
	return f.detail, nil
}
func (f *fakeProductService) List(_ context.Context, _ *productdto.ListReq) ([]*productdto.ProductResp, error) {
	return f.listed, f.err
}
func (f *fakeProductService) ListCategories(_ context.Context, _ *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	return f.cats, f.err
}
func (f *fakeProductService) ListBrands(_ context.Context, _ *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	return f.brands, f.err
}
func (f *fakeProductService) GetCategory(_ context.Context, req *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, row := range f.cats {
		if row != nil && row.ID == req.ID {
			return row, nil
		}
	}
	return nil, productenums.Err(productenums.ErrCategoryNotFound)
}
func (f *fakeProductService) GetBrand(_ context.Context, req *productdto.GetBrandReq) (*productdto.BrandResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, row := range f.brands {
		if row != nil && row.ID == req.ID {
			return row, nil
		}
	}
	return nil, productenums.Err(productenums.ErrBrandNotFound)
}

func postScoreHTML(t *testing.T, handler gin.HandlerFunc, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure), pageview.UseSuccess(pageview.NewSuccessWriter()), templates.FailedRenderMiddleware(pageview.Fail))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/score", pageview.HTMLFragment, handler)
	req := httptest.NewRequest(http.MethodPost, "/score", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "</div>") {
		t.Fatalf("评分必须返回真实 HTML：%s", rec.Body.String())
	}
	for _, header := range []string{"Location", "HX-Redirect", "HX-Location"} {
		if rec.Header().Get(header) != "" {
			t.Fatalf("评分不得暗示导航：%s", header)
		}
	}
	return rec
}

func TestScorePanelsReturnEntityFacts(t *testing.T) {
	fake := &fakeProductService{
		detail: &productdto.ProductResp{ID: "p1", ProjectID: "proj-1", Name: "纯棉T恤", Slug: "tee"},
		listed: []*productdto.ProductResp{{ID: "p2", ProjectID: "proj-1", Name: "纯棉T恤", Slug: "tee-b"}},
		cats:   []*productdto.CategoryResp{{ID: "c1", ProjectID: "proj-1", Name: "男装", Slug: "men"}},
		brands: []*productdto.BrandResp{{ID: "b1", ProjectID: "proj-1", Name: "示例品牌", Slug: "brand"}},
	}
	h := NewProductPageHandle(fake, nil)
	cases := []struct {
		id, kind, profile, title, path string
		handler                        gin.HandlerFunc
	}{
		{"p1", "product", "product", "纯棉T恤", "/tee", h.SeoScore},
		{"c1", "category", "landing", "男装", "/men", h.CategorySeoScore},
		{"b1", "brand", "landing", "示例品牌", "/brand", h.BrandSeoScore},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			rec := postScoreHTML(t, tc.handler, url.Values{"projectId": {"proj-1"}, "id": {tc.id}})
			if rec.Code != http.StatusOK {
				t.Fatalf("正常评分失败：%d %s", rec.Code, rec.Body.String())
			}
			for _, want := range []string{"data-product-seo-panel", "data-seo-sections", tc.title, tc.path, "</section>"} {
				if !strings.Contains(rec.Body.String(), want) {
					t.Fatalf("HTML 缺少评分事实 %q：%s", want, rec.Body.String())
				}
			}
			if tc.kind == "product" && !strings.Contains(rec.Body.String(), "重复的 title（纯棉T恤）出现在 1 个页面") {
				t.Fatalf("重复标题事实丢失：%s", rec.Body.String())
			}
			for _, forbidden := range []string{`"color":`, `"grade":`, `"targetId":`, `"hiddenFields":`, `"actual":`, `"target":`, `"html":`, `"profileReason":`} {
				if strings.Contains(rec.Body.String(), forbidden) {
					t.Fatalf("响应仍携带展示字段 %s：%s", forbidden, rec.Body.String())
				}
			}
		})
	}
}

func TestScorePanelsReportFailureStatusAndNeverExposeInternalError(t *testing.T) {
	for _, kind := range []string{"product", "category", "brand"} {
		for _, tc := range []struct {
			name, id, project string
			err               error
			status            int
		}{
			{"缺ID", "", "proj-1", nil, 400},
			{"缺工程", "e1", "", nil, 400},
			{"实体不存在", "e1", "proj-1", nil, 404},
			{"数据库失败", "e1", "proj-1", errors.New("postgres password=private database failed"), 500},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				h := NewProductPageHandle(&fakeProductService{err: tc.err}, nil)
				handler := h.SeoScore
				if kind == "category" {
					handler = h.CategorySeoScore
				}
				if kind == "brand" {
					handler = h.BrandSeoScore
				}
				rec := postScoreHTML(t, handler, url.Values{"projectId": {tc.project}, "id": {tc.id}})
				if rec.Code != tc.status {
					t.Fatalf("状态码=%d，期望%d：%s", rec.Code, tc.status, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), `data-ui-error`) || !strings.Contains(rec.Body.String(), `role="alert"`) || strings.Contains(rec.Body.String(), "password=private") {
					t.Fatalf("错误必须有对外文案且不得泄露内部原文：%s", rec.Body.String())
				}
			})
		}
	}
}
