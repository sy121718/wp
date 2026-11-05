package feature

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/migrations"
)

// TestProductTaxonomyShortKeysRenderRealLanguages 验证完整短 key 的真实中英文命中，以及三个页面和嵌套表单的实际渲染。
func TestProductTaxonomyShortKeysRenderRealLanguages(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		t.Fatal("分类属性品牌词条验收必须使用真实 PostgreSQL")
	}
	keys := map[string]bool{"catMatchInfo": false}
	oldKeys := map[string]bool{}
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == "720-product-taxonomy-short-keys-i18n" {
			for _, match := range regexp.MustCompile(`\('([^']+)', '([^']+)', (?:true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
				oldKeys[match[1]] = true
				keys[match[2]] = false
			}
		}
	}
	if len(keys) != 110 {
		t.Fatalf("109 个改名和一个完整句的范围改变: %d", len(keys))
	}
	paths, err := filepath.Glob(filepath.Join(attrTemplateRoot(), "admin/product/*.html"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("找不到真实商品模板: %v", err)
	}
	// 同时覆盖直接调用、data 取词和局部取词别名，避免只扫 t/tr 漏掉表单。
	call := regexp.MustCompile(`(?:\b(?:t|tr|tf|tn|gfTr|vfTr)|\.\["t"\])\s*\(\s*"([^"]+)"`)
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range call.FindAllStringSubmatch(string(source), -1) {
			if oldKeys[match[1]] {
				t.Fatalf("商品模板仍消费本批旧 key: %s: %s", path, match[1])
			}
			if _, ok := keys[match[1]]; ok {
				keys[match[1]] = true
			}
		}
		if filepath.Base(path) == "product_categories.html" && (strings.Contains(string(source), "admin.product_categories.search.matchCount") || strings.Contains(string(source), "admin.product_categories.search.matchSuffix")) {
			t.Fatal("分类匹配结果仍在拼接碎片词条")
		}
	}
	for key, used := range keys {
		if !used {
			t.Fatalf("本批词条没有真实模板消费者: %s", key)
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			var want string
			if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ? AND status = 1", key, lang).Scan(&want).Error; err != nil {
				t.Fatal(err)
			}
			if got := i18n.Get(key, lang); want == "" || got.Lang != lang || got.Value != want {
				t.Fatalf("未命中指定语言的真实译文: %s/%s: %+v", key, lang, got)
			}
		}
	}
	if _, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "<Attribute>", Key: "color", Values: []productdto.AttributeValueReq{{Label: "<Red>", Key: "red"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateCategory(t.Context(), &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "<Category>", Slug: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "<Brand>", Slug: "brand"}); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) {
		text.AttachScope(c)
		buttons, permissions := map[string]bool{}, map[string]bool{}
		for _, kind := range []string{"attribute", "category", "brand"} {
			for _, action := range []string{"create", "update", "delete"} {
				buttons["product."+kind+"_"+action] = true
				permissions["product:"+kind+"_"+action] = true
			}
		}
		c.Set(adminctx.ButtonsKey, buttons)
		c.Set(adminctx.PermSetKey, permissions)
		c.Next()
	})
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/product-attributes", pageview.UseRenderer(adminui.RenderPage), handle.ProductAttributesPage)
	engine.GET("/admin/product-categories", pageview.UseRenderer(adminui.RenderPage), handle.ProductCategoriesPage)
	engine.GET("/admin/product-brands", pageview.UseRenderer(adminui.RenderPage), handle.ProductBrandsPage)
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			for _, tc := range []struct {
				path, entity string
				words        []string
			}{
				{"/admin/product-attributes", "<Attribute>", []string{"attrTitle", "attrGroupNameEg", "attrKeyEg", "attrSaveValues", "attrValsAdd", "uiActCancel"}},
				{"/admin/product-categories", "<Category>", []string{"catTitle", "catNewSubmit", "catListSave", "catParentFind", "catOptTopLevel", "uiActCancel"}},
				{"/admin/product-brands", "<Brand>", []string{"brandTitle", "brandNewSubmit", "brandListSave", "brandNameLabel", "uiActCancel"}},
			} {
				query := url.Values{"project": {f.projectID}}
				if tc.path == "/admin/product-categories" {
					query.Set("keyword", "<Category>")
				}
				req := httptest.NewRequest(http.MethodGet, tc.path+"?"+query.Encode(), nil)
				req.Header.Set("Accept-Language", lang)
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				out := rec.Body.String()
				if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(out, "</html>") || !strings.Contains(out, "</form>") {
					t.Fatalf("真实页面或嵌套表单渲染失败: %s: %d %s", tc.path, rec.Code, out)
				}
				if !strings.Contains(out, html.EscapeString(tc.entity)) || strings.Contains(out, tc.entity) {
					t.Fatalf("业务名称未正确转义: %s", tc.path)
				}
				for _, key := range tc.words {
					if !strings.Contains(out, html.EscapeString(i18n.GetText(key, lang))) {
						t.Fatalf("页面未展示指定语言译文: %s: %s/%s", tc.path, key, lang)
					}
				}
				if tc.path == "/admin/product-categories" {
					want := html.EscapeString(strings.ReplaceAll(i18n.GetText("catMatchInfo", lang), "{count}", "1"))
					if strings.Count(out, want) != 1 || strings.Contains(out, "{count}") {
						t.Fatalf("分类结果没有且仅有一条完整参数句: %s", out)
					}
				}
			}
		})
	}
}
