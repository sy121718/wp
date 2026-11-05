package feature

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/pkg/auth"
	pageview "go_wp/pkg/view"
)

// TestProductSEORealBrowser 验证真实 PostgreSQL、Gin、Jet 与 Chrome 上的只读评分交互。
func TestProductSEORealBrowser(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 运行 SEO 三视口真实浏览器验收")
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = auth.Close() })
	engine, f := newCreateFlowEngine(t, builtin.CSRFMiddleware(), func(c *gin.Context) {
		if c.Request.Method == "POST" {
			time.Sleep(250 * time.Millisecond)
		}
		c.Next()
	})
	if engine == nil {
		t.Fatal("SEO 浏览器专项必须使用真实 PostgreSQL")
	}
	p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "浏览器 SEO 商品", Slug: "seo-browser-product"})
	if err != nil {
		t.Fatal(err)
	}
	category, err := f.svc.CreateCategory(t.Context(), &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "浏览器 SEO 分类", Slug: "seo-browser-category"})
	if err != nil {
		t.Fatal(err)
	}
	brand, err := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "浏览器 SEO 品牌", Slug: "seo-browser-brand"})
	if err != nil {
		t.Fatal(err)
	}
	beforeProduct, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeCategory, err := f.svc.GetCategory(t.Context(), &productdto.GetCategoryReq{ProjectID: f.projectID, ID: category.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeBrand, err := f.svc.GetBrand(t.Context(), &productdto.GetBrandReq{ProjectID: f.projectID, ID: brand.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeWrites := map[string]int{}
	for _, table := range []string{"product_outbox_events", "presentation_instances", "presentation_artifacts", "document_snapshots"} {
		beforeWrites[table] = countRows(t, f.db, table, "")
	}
	// 仅夹具授予明确权限，不替代生产登录与 Casbin 验收。
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"product:update": true, "product:seo_score": true, "product:category_seo_score": true, "product:brand_seo_score": true})
		buttons := map[string]bool{"product.update": true}
		if c.Query("readonly") != "1" {
			buttons["product.category_update"], buttons["product.brand_update"] = true, true
		}
		c.Set(adminctx.ButtonsKey, buttons)
		c.Next()
	})
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/products/edit", pageview.UseRenderer(adminui.RenderPage), handle.ProductEditPage)
	engine.GET("/admin/product-categories", pageview.UseRenderer(adminui.RenderPage), handle.ProductCategoriesPage)
	engine.GET("/admin/product-brands", pageview.UseRenderer(adminui.RenderPage), handle.ProductBrandsPage)
	engine.Static("/static", attrTemplateRoot()+"/static")
	server := httptest.NewServer(engine)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "testdata/product-seo-browser.cjs")
	cmd.Env = append(os.Environ(), "GOWP_PRODUCT_BASE_URL="+server.URL, "GOWP_PRODUCT_PROJECT_ID="+f.projectID,
		"GOWP_SEO_PRODUCT="+p.ID, "GOWP_SEO_CATEGORY="+category.ID, "GOWP_SEO_BRAND="+brand.ID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SEO 真实浏览器验收失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
	afterProduct, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil || !reflect.DeepEqual(beforeProduct, afterProduct) {
		t.Fatalf("浏览器 SEO 不得保存商品：%v", err)
	}
	afterCategory, err := f.svc.GetCategory(t.Context(), &productdto.GetCategoryReq{ProjectID: f.projectID, ID: category.ID})
	if err != nil || !reflect.DeepEqual(beforeCategory, afterCategory) {
		t.Fatalf("浏览器 SEO 不得保存分类：%v", err)
	}
	afterBrand, err := f.svc.GetBrand(t.Context(), &productdto.GetBrandReq{ProjectID: f.projectID, ID: brand.ID})
	if err != nil || !reflect.DeepEqual(beforeBrand, afterBrand) {
		t.Fatalf("浏览器 SEO 不得保存品牌：%v", err)
	}
	for table, before := range beforeWrites {
		if after := countRows(t, f.db, table, ""); after != before {
			t.Fatalf("浏览器 SEO 不得写入 %s：before=%d after=%d", table, before, after)
		}
	}
}
