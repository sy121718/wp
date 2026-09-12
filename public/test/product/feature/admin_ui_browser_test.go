package feature

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// 真实业务服务、生产迁移与页面模板；数据只写隔离测试 schema。
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
	if _, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "精选商品", Slug: "featured-products", Kind: "manual"}); err != nil {
		t.Fatal(err)
	}
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
	router.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	h := dashboardhttp.NewProductPageHandle(f.svc, f.projects)
	router.GET("/admin/product-brands", h.ProductBrandsPage)
	router.POST("/admin/product-brands/create", h.ProductBrandsCreate)
	router.POST("/admin/product-brands/update", h.ProductBrandsUpdate)
	router.POST("/admin/product-brands/delete", h.ProductBrandsDelete)
	router.GET("/admin/product-categories", h.ProductCategoriesPage)
	router.POST("/admin/product-categories/create", h.ProductCategoriesCreate)
	router.POST("/admin/product-categories/update", h.ProductCategoriesUpdate)
	router.POST("/admin/product-categories/delete", h.ProductCategoriesDelete)
	router.GET("/admin/product-tags", h.ProductTagsPage)
	router.POST("/admin/product-tags/create", h.ProductTagsCreate)
	router.POST("/admin/product-tags/update", h.ProductTagsUpdate)
	router.POST("/admin/product-tags/delete", h.ProductTagsDelete)
	router.POST("/admin/product-tags/recalc", h.ProductTagsRecalc)
	router.Static("/static", attrTemplateRoot()+"/static")
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
			if !strings.Contains(body, `class="form-input"`) || !strings.Contains(body, `class="disclosure"`) {
				t.Fatal("页面未接公共表单与折叠组件")
			}
		})
	}
}

// GOWP_ADMIN_UI_BROWSER=1 go test ./public/test/product/feature -run TestAdminUIBrowserFixture -v -timeout 25m
// 仅用于本机浏览器验收，不包含生产认证链路，也不会访问开发数据库业务行。
func TestAdminUIBrowserFixture(t *testing.T) {
	if os.Getenv("GOWP_ADMIN_UI_BROWSER") != "1" {
		t.Skip("按需启动后台浏览器夹具")
	}
	router := newTaxonomyUIFixture(t)
	if router == nil {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:19127")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 3 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("后台浏览器夹具：http://127.0.0.1:19127/admin/product-brands")
	<-time.After(20 * time.Minute)
}
