package producthttp

// product_seo_score_handler_test.go — 三个评分端点的行为测试（审计 SEO-016 / SEO-018）。
//
// 原 dashboard/inbound/http/seo_entity_score_handler_test.go，随商品评分端点搬回本模块。
//
// 与 seo_entity_score_test.go 的分工：那边测纯逻辑与模板，这边测**端点自己**——
// 取数、按页型选权重、把冲突页面渲染进响应。SEO-016 的根因是「评分器写好了但生产
// 入口没接线」，只测评分函数是测不出这种缺口的，必须从端点这一层打进去。
//
// 数据源用一个只实现所需方法的假实现（接口嵌入 + 覆盖几个只读方法），
// 不碰数据库 —— 与模块内就近单测的定位一致。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
)

// fakeProductService 只实现评分入口用到的只读方法；其余方法由嵌入的接口兜底
// （本组测试不会调到它们，写 40 个空方法只会变成噪音）。
type fakeProductService struct {
	productcontract.ProductService
	detail *productdto.ProductResp
	listed []*productdto.ProductResp
	cats   []*productdto.CategoryResp
	brands []*productdto.BrandResp
}

func (f *fakeProductService) Get(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	if f.detail == nil || req.ID != f.detail.ID {
		return nil, errors.New("not found")
	}
	return f.detail, nil
}

func (f *fakeProductService) List(_ context.Context, _ *productdto.ListReq) ([]*productdto.ProductResp, error) {
	return f.listed, nil
}

func (f *fakeProductService) ListCategories(_ context.Context, _ *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	return f.cats, nil
}

func (f *fakeProductService) ListBrands(_ context.Context, _ *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	return f.brands, nil
}

func (f *fakeProductService) GetCategory(_ context.Context, req *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
	for _, c := range f.cats {
		if c != nil && c.ID == req.ID {
			return c, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeProductService) GetBrand(_ context.Context, req *productdto.GetBrandReq) (*productdto.BrandResp, error) {
	for _, b := range f.brands {
		if b != nil && b.ID == req.ID {
			return b, nil
		}
	}
	return nil, errors.New("not found")
}

// postScorePanel 向评分端点发一个表单 POST 并返回响应 HTML。
func postScorePanel(t *testing.T, handler gin.HandlerFunc, form url.Values) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.POST("/score", handler)
	req := httptest.NewRequest(http.MethodPost, "/score", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("评分端点应返回 200（HTMX 片段），实际 %d", rec.Code)
	}
	return rec.Body.String()
}

// TestProductScorePanelUsesProductProfile 商品端点按商品页权重评分（SEO-016 的核心验收）。
func TestProductScorePanelUsesProductProfile(t *testing.T) {
	fake := &fakeProductService{
		detail: &productdto.ProductResp{
			ID: "p1", ProjectID: "proj-1", Name: "纯棉 T 恤", Slug: "tee",
			SEOTitle: "", SEODescription: "", Description: []byte(`"透气亲肤的夏季基础款"`),
			Images: []string{"/img/tee.webp"}, ImageAlts: []string{"白色纯棉 T 恤"},
		},
	}
	h := NewProductPageHandle(fake, nil)
	body := postScorePanel(t, h.ProductScorePanel, url.Values{
		"projectId": {"proj-1"}, "productId": {"p1"},
	})
	for _, want := range []string{
		"SEO 评分（0-100）",
		"页型权重：product",  // 页型回显（文档 §5 要求）
		"商品页：图片与技术权重上调", // 调权理由
		"纯棉 T 恤",        // SERP 预览用实体名回落
		"/tee",          // slug 构成的近似路径
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("商品评分片段应包含 %q，实际输出：%s", want, body)
		}
	}
}

// TestProductScorePanelListsTitleConflicts 商品端点列出同工程已发布商品的标题冲突（SEO-018）。
func TestProductScorePanelListsTitleConflicts(t *testing.T) {
	fake := &fakeProductService{
		detail: &productdto.ProductResp{ID: "p1", ProjectID: "proj-1", Name: "纯棉 T 恤", Slug: "tee"},
		// 同工程另一个已发布商品用了同一个 SEO 标题（名称回落口径一致才比得出来）。
		listed: []*productdto.ProductResp{
			{ID: "p2", ProjectID: "proj-1", Name: "纯棉 T 恤", Slug: "tee-b", Status: "published"},
		},
	}
	h := NewProductPageHandle(fake, nil)
	body := postScorePanel(t, h.ProductScorePanel, url.Values{
		"projectId": {"proj-1"}, "productId": {"p1"},
	})
	for _, want := range []string{"标题重复", "/tee-b", "重复的 title（纯棉 T 恤）出现在 1 个页面：/tee-b"} {
		if !strings.Contains(body, want) {
			t.Fatalf("标题冲突应被检出并列出冲突页面，期望包含 %q，实际输出：%s", want, body)
		}
	}
}

// TestBrandScorePanelUsesLandingProfile 品牌端点按落地页权重评分。
func TestBrandScorePanelUsesLandingProfile(t *testing.T) {
	fake := &fakeProductService{
		brands: []*productdto.BrandResp{{ID: "b1", ProjectID: "proj-1", Name: "示例品牌", Slug: "demo"}},
	}
	h := NewProductPageHandle(fake, nil)
	body := postScorePanel(t, h.ProductBrandScorePanel, url.Values{
		"projectId": {"proj-1"}, "id": {"b1"},
	})
	for _, want := range []string{"页型权重：landing", "示例品牌", "/demo"} {
		if !strings.Contains(body, want) {
			t.Fatalf("品牌评分片段应包含 %q，实际输出：%s", want, body)
		}
	}
}

// TestCategoryScorePanelUsesLandingProfile 分类端点按落地页权重评分。
func TestCategoryScorePanelUsesLandingProfile(t *testing.T) {
	fake := &fakeProductService{
		cats: []*productdto.CategoryResp{{ID: "c1", ProjectID: "proj-1", Name: "男装", Slug: "men"}},
	}
	h := NewProductPageHandle(fake, nil)
	body := postScorePanel(t, h.ProductCategoryScorePanel, url.Values{
		"projectId": {"proj-1"}, "id": {"c1"},
	})
	for _, want := range []string{"页型权重：landing", "男装", "/men"} {
		if !strings.Contains(body, want) {
			t.Fatalf("分类评分片段应包含 %q，实际输出：%s", want, body)
		}
	}
}

// TestProductScorePanelMissingEntityIsReadable 读不到实体时给一句可读的话，不是 500。
func TestProductScorePanelMissingEntityIsReadable(t *testing.T) {
	h := NewProductPageHandle(&fakeProductService{}, nil)
	body := postScorePanel(t, h.ProductScorePanel, url.Values{"projectId": {"proj-1"}, "productId": {"ghost"}})
	if !strings.Contains(body, "读不到这个商品") {
		t.Fatalf("空态应给出可读文案，实际输出：%s", body)
	}
}
