package producthttp

import (
	json "go_wp/pkg/jsonx"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

func TestProductScorePanelsAreWiredIntoAdminTemplates(t *testing.T) {

	product := productRowForRender()
	products := renderAdminTemplate(t, "admin/product/product_edit.html", productPageLayoutData(gin.H{
		"title": "编辑商品", "menu": "products", "PermSet": map[string]bool{"product:seo_score": true},
		"Scope": &productcontract.CatalogScope{ProjectID: "proj-1"}, "Attributes": []*productdto.AttributeResp{}, "Categories": []*productdto.CategoryResp{}, "Brands": []*productdto.BrandResp{}, "Tags": []*productdto.TagResp{},
		"WarehouseOptions": []gin.H{}, "Err": "",
		"HasProduct": true, "ProductID": "p1", "BackURL": "/admin/products",
		"Product":         product,
		"Statuses":        []gin.H{{"Value": "draft", "LabelKey": "admin.products.status.draft", "LabelFallback": "草稿", "Selected": true}},
		"AttributeChecks": []gin.H{},
		"ImagesInput":     "", "ImageAltsInput": "", "WeightInput": "", "DefaultPriceInput": "",
	}))

	for _, want := range []string{
		`data-drawer-open="#tpl-product-seo"`,
		`hx-post="/api/product/seo-score"`,
		`name="id" value="p1"`,
		"SEO 评分",
	} {
		if !strings.Contains(products, want) {
			t.Fatalf("商品编辑页应包含 %q，实际输出：%s", want, products)
		}
	}
	if strings.Contains(products, `id="product-seo-score-p1"`) {
		t.Error("评分容器不应依赖后端生成的实体 DOM id")
	}

	categories := renderAdminTemplate(t, "admin/product/product_categories.html", productPageLayoutData(gin.H{
		"title": "商品分类", "menu": "product-categories",
		"Scope": &productcontract.CatalogScope{ProjectID: "proj-1"}, "Query": &productdto.CatalogTaxonomyReq{}, "Result": &productdto.CategorySearchResp{Page: 1, Size: 20, Items: []*productdto.CategoryResp{{ID: "c1", Name: "男装", Slug: "men"}}}, "Categories": []*productdto.CategoryResp{}, "req_path": "/admin/product-categories",

		"Buttons": map[string]any{"product.category_update": true},
		"PermSet": map[string]bool{"product:category_seo_score": true},
		"Err":     "",
	}))
	for _, want := range []string{`hx-post="/api/product/category/seo-score"`, `hx-target="find [data-write-result]"`} {
		if !strings.Contains(categories, want) {
			t.Fatalf("分类页应包含 %q，实际输出：%s", want, categories)
		}
	}

	brands := renderAdminTemplate(t, "admin/product/product_brands.html", productPageLayoutData(gin.H{
		"title": "商品品牌", "menu": "product-brands",
		"Scope": &productcontract.CatalogScope{ProjectID: "proj-1"}, "Query": &productdto.CatalogTaxonomyReq{}, "Result": &productdto.TaxonomyListResp[*productdto.BrandResp]{Page: 1, Size: 20, Items: []*productdto.BrandResp{{ID: "b1", Name: "示例品牌", Slug: "demo"}}}, "req_path": "/admin/product-brands",

		"Buttons": map[string]any{"product.brand_update": true},
		"PermSet": map[string]bool{"product:brand_seo_score": true},
		"Err":     "",
	}))
	for _, want := range []string{`hx-post="/api/product/brand/seo-score"`, `hx-target="find [data-write-result]"`} {
		if !strings.Contains(brands, want) {
			t.Fatalf("品牌页应包含 %q，实际输出：%s", want, brands)
		}
	}
}

// TestProductSEOIndependentPermissions 验证评分权限独立于编辑权限，缺失或显式拒绝均不能显示入口。
func TestProductSEOIndependentPermissions(t *testing.T) {
	for _, kind := range []string{"product", "category", "brand"} {
		permission, path, trigger := "product:seo_score", "/api/product/seo-score", "#tpl-product-seo"
		if kind != "product" {
			permission = "product:" + kind + "_seo_score"
			path, trigger = "/api/product/"+kind+"/seo-score", "#tpl-"+kind+"-edit-"+kind+"1"
		}
		for _, tc := range []struct {
			name         string
			perms        map[string]bool
			update, want bool
		}{
			{"缺失", nil, true, false},
			{"显式拒绝", map[string]bool{permission: false}, true, false},
			{"其他评分权限", map[string]bool{"product:seo_score": kind != "product", "product:category_seo_score": kind != "category", "product:brand_seo_score": kind != "brand"}, true, false},
			{"仅评分权限", map[string]bool{permission: true}, false, true},
			{"评分与编辑权限", map[string]bool{permission: true}, true, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				data := productPageLayoutData(gin.H{
					"title": "SEO 权限", "menu": "products", "Err": "", "PermSet": tc.perms,
					"Scope":   &productcontract.CatalogScope{ProjectID: "proj-1"},
					"Buttons": map[string]any{}, "Query": &productdto.CatalogTaxonomyReq{},
					"req_path": "/admin/products", "Categories": []*productdto.CategoryResp{},
				})
				template := "admin/product/product_edit.html"
				switch kind {
				case "product":
					data["Product"], data["HasProduct"], data["ProductID"] = productRowForRender(), true, "p1"
					data["Attributes"], data["Brands"], data["Tags"] = []*productdto.AttributeResp{}, []*productdto.BrandResp{}, []*productdto.TagResp{}
					data["WarehouseOptions"], data["AttributeChecks"] = []gin.H{}, []gin.H{}
					data["BackURL"], data["Statuses"] = "/admin/products", []gin.H{}
					for _, key := range []string{"ImagesInput", "ImageAltsInput", "WeightInput", "DefaultPriceInput"} {
						data[key] = ""
					}
				case "category":
					template = "admin/product/product_categories.html"
					data["Result"] = &productdto.CategorySearchResp{Page: 1, Size: 20, Items: []*productdto.CategoryResp{{ID: "category1", Name: "分类"}}}
				case "brand":
					template = "admin/product/product_brands.html"
					data["Result"] = &productdto.TaxonomyListResp[*productdto.BrandResp]{Page: 1, Size: 20, Items: []*productdto.BrandResp{{ID: "brand1", Name: "品牌"}}}
				}
				if tc.update {
					data["Buttons"].(map[string]any)["product."+kind+"_update"] = true
				}
				out := renderAdminTemplate(t, template, data)
				if got := strings.Contains(out, `hx-post="`+path+`"`); got != tc.want {
					t.Fatalf("评分表单授权不符：got=%v want=%v", got, tc.want)
				}
				if tc.want && !strings.Contains(out, `data-drawer-open="`+trigger+`"`) {
					t.Fatal("有独立评分权限却没有可达的抽屉入口")
				}
				if !tc.update && kind != "product" && strings.Contains(out, `data-write-url="/api/product/`+kind+`/update"`) {
					t.Fatal("评分权限不能授予编辑表单")
				}
			})
		}
	}
}

func testTranslateFunc() func(key, fallback string) string {

	return func(key, fallback string) string {
		if fallback != "" {
			return fallback
		}
		return key
	}
}

func productRowForRender() *productdto.ProductResp {
	attr := &productdto.AttributeResp{ID: "a1", Key: "color", Name: "颜色", IsVariation: true, Values: []productdto.AttributeValueResp{}}
	return &productdto.ProductResp{
		ID: "p1", ProjectID: "p1", Name: "测试商品", Slug: "tee", Status: "published", PriceMin: 59, PriceMax: 99, VariantCount: 1,
		Variants:   []*productdto.VariantResp{{ID: "v1", SKUCode: "SZ_TEE_001", Price: 59, Enabled: true, StockTotal: 10, OptionValues: json.RawMessage("{}")}},
		Attributes: []*productdto.AttributeResp{attr}, Categories: []*productdto.CategoryResp{{ID: "c1", Name: "男装"}}, Rating: &productdto.RatingResp{ProjectID: "p1", ProductID: "p1"},
	}
}

func TestProductEditRendersMediaGallery(t *testing.T) {
	product := productRowForRender()
	out := renderAdminTemplate(t, "admin/product/product_edit.html", productPageLayoutData(gin.H{
		"title": "编辑商品", "menu": "products",
		"Scope": &productcontract.CatalogScope{ProjectID: "proj-1"}, "Attributes": []*productdto.AttributeResp{}, "Categories": []*productdto.CategoryResp{}, "Brands": []*productdto.BrandResp{}, "Tags": []*productdto.TagResp{},
		"WarehouseOptions": []gin.H{}, "Err": "",
		"HasProduct": true, "ProductID": "p1", "BackURL": "/admin/products",
		"Product":         product,
		"Statuses":        []gin.H{{"Value": "draft", "LabelKey": "admin.products.status.draft", "LabelFallback": "草稿", "Selected": true}},
		"AttributeChecks": []gin.H{},
		"ImagesInput":     "http://127.0.0.1:8080/storage/372.webp",
		"ImageAltsInput":  "示例 alt",
		"WeightInput":     "", "DefaultPriceInput": "",
	}))
	for _, want := range []string{
		"data-media-gallery", "data-gallery-grid", "data-gallery-addbox",
		`name="images"`, `name="imageAlts"`,
		"data-gallery-values", "data-gallery-alts",

		`name="defaultImage"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("商品编辑页的图集控件缺少 %q", want)
		}
	}
	if strings.Contains(out, `id="product-edit-images"`) {
		t.Error("图集仍是裸 textarea：应换成多图控件")
	}

	if n, box := strings.Count(out, "data-gallery-add"), strings.Count(out, "data-gallery-addbox"); n != box {
		t.Errorf("图集的添加入口应只有「＋」框一个：data-gallery-add 出现 %d 次、data-gallery-addbox 出现 %d 次", n, box)
	}
	if strings.Contains(out, "media-pair") {
		t.Error("主图与图集应各占一整行：并排后屏幕上并排两个虚线「＋」框，像同一个控件的两个格子")
	}
}

func TestTaxonomyFormsUseMediaField(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		field string
	}{
		{"分类图", "../../../../../internal/templates/admin/product/product_category_form.html", "image"},

		{"品牌 logo", "../../../../../internal/templates/admin/product/product_brands.html", "logo"},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("读模板源失败：%v", err)
		}
		src := string(raw)
		for _, want := range []string{"media_field.html", "yield mediaField(", `field="` + tc.field + `"`} {
			if !strings.Contains(src, want) {
				t.Errorf("%s 的媒体字段缺少 %q", tc.name, want)
			}
		}

		if strings.Contains(src, `type="file"`) {
			t.Errorf("%s 出现了文件输入：上传与选择只该走媒体库", tc.name)
		}
	}
}
