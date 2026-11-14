package feature

import (
	"context"
	pageview "go_wp/pkg/view"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go_wp/internal/middleware/adminctx"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	"github.com/gin-gonic/gin"
)

func newTaxonomyUIFixture(t *testing.T) *gin.Engine {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil
	}
	ctx := context.Background()
	if _, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "空白测试工程"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "山野测试品牌", Slug: "outdoor-brand-with-a-long-readable-slug", SEOTitle: "品牌页面标题"}); err != nil {
		t.Fatal(err)
	}
	root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "户外服装", Slug: "outdoor-clothing", SEOTitle: "户外分类标题"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "轻量冲锋衣", Slug: "lightweight-jackets", ParentID: root.ID}); err != nil {
		t.Fatal(err)
	}
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "精选商品", Slug: "featured-products", Kind: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	seedTagProducts(t, f.db, f.projectID, tag.ID, "浏览器验收商品", 23)
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.Request.URL.Path == "/admin/product-brands" || c.Request.URL.Path == "/admin/product-categories" || c.Request.URL.Path == "/admin/product-tags" {
			if c.Query("project") == "" {
				q := c.Request.URL.Query()
				q.Set("project", f.projectID)
				c.Request.URL.RawQuery = q.Encode()
			}
		}
		c.Next()
	})

	router.Use(pageview.FailureMiddleware(pageview.RenderFailure), templates.FailedRenderMiddleware(pageview.Fail))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:brand_create": true, "product:brand_update": true, "product:brand_delete": true,
			"product:category_create": true, "product:category_update": true, "product:category_delete": true,
			"product:tag_create": true, "product:tag_update": true, "product:tag_delete": true,
			"product:category_seo_score": true, "product:brand_seo_score": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.brand_create": true, "product.brand_update": true, "product.brand_delete": true,
			"product.category_create": true, "product.category_update": true, "product.category_delete": true,
			"product.tag_create": true, "product.tag_update": true, "product.tag_delete": true,
		})

	})
	router.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	h := producthttp.NewProductPageHandle(f.svc, f.svc)
	router.GET("/admin/product-brands", pageview.UseRenderer(pageview.RenderPage), h.ProductBrandsPage)
	router.GET("/admin/product-categories", pageview.UseRenderer(pageview.RenderPage), h.ProductCategoriesPage)
	router.GET("/admin/product-tags", pageview.UseRenderer(pageview.RenderPage), h.ProductTagsPage)

	write := producthttp.NewProductHandle(f.svc)
	router.GET("/api/product/category/parents", write.ProductCategoryParents)
	router.GET("/api/product/tag/products", write.ListTagProducts)
	router.POST("/api/product/category/seo-score", pageview.HTMLFragment, h.CategorySeoScore)
	router.POST("/api/product/brand/seo-score", pageview.HTMLFragment, h.BrandSeoScore)
	router.POST("/api/product/category/update", write.UpdateCategory)
	router.POST("/api/product/brand/update", write.UpdateBrand)
	mountStaticAssets(router)
	return router
}

func TestTaxonomyPagesUsePublicUI(t *testing.T) {
	router := newTaxonomyUIFixture(t)
	if router == nil {
		return
	}
	for _, path := range []string{"product-brands", "product-categories", "product-tags"} {
		t.Run(path, func(t *testing.T) {
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/admin/"+path, nil))
			body := res.Body.String()
			if res.Code != http.StatusOK || !strings.Contains(body, "</html>") {
				t.Fatalf("页面渲染不完整：%d %s", res.Code, body)
			}
			if strings.Contains(body, `class="pages-`) || strings.Contains(body, `class="attr-form`) {
				t.Fatal("页面仍依赖旧类")
			}

			if !strings.Contains(body, `class="form-input"`) || !strings.Contains(body, `class="help-pop"`) {
				t.Fatal("页面未接公共表单控件与页头悬浮说明")
			}

			for _, want := range []string{`class="data-table"`, `class="col-check"`, "data-check-all", `class="bulk-bar"`} {
				if !strings.Contains(body, want) {
					t.Fatalf("列表页缺少标准列表骨架 %q", want)
				}
			}
		})
	}
}

func TestAdminUIBrowserFixture(t *testing.T) {
	if os.Getenv("GOWP_ADMIN_UI_BROWSER") != "1" {
		t.Skip("按需启动后台浏览器夹具")
	}
	router := newTaxonomyUIFixture(t)
	if router == nil {
		return
	}
	addr := os.Getenv("GOWP_ADMIN_UI_BROWSER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:19127"
	}
	duration := 20 * time.Minute
	if value := os.Getenv("GOWP_ADMIN_UI_BROWSER_DURATION"); value != "" {
		var err error
		duration, err = time.ParseDuration(value)
		if err != nil || duration <= 0 {
			t.Fatal("浏览器夹具持续时间必须为正时长")
		}
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 3 * time.Second}
	defer server.Close()

	// 常驻前自检一次：夹具站点至少要能渲染列表页骨架，
	// 否则打开浏览器看到的是 500 或空壳，事后排查会误判成前端问题。
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin/product-brands", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `class="data-table"`) {
		t.Fatalf("后台夹具自检失败：GET /admin/product-brands = %d", page.Code)
	}

	go server.Serve(listener)
	t.Logf("后台浏览器夹具：http://%s/admin/product-brands（%s 后自动退出）", listener.Addr(), duration)
	<-time.After(duration)
}
