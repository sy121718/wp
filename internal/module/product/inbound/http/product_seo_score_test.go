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
		"title": "编辑商品", "menu": "products",
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
		`data-write-url="/api/product/seo-score"`,
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
		"Err":     "",
	}))
	for _, want := range []string{`data-write-url="/api/product/category/seo-score"`, `data-write-handler="product.seo"`} {
		if !strings.Contains(categories, want) {
			t.Fatalf("分类页应包含 %q，实际输出：%s", want, categories)
		}
	}

	brands := renderAdminTemplate(t, "admin/product/product_brands.html", productPageLayoutData(gin.H{
		"title": "商品品牌", "menu": "product-brands",
		"Scope": &productcontract.CatalogScope{ProjectID: "proj-1"}, "Query": &productdto.CatalogTaxonomyReq{}, "Result": &productdto.TaxonomyListResp[*productdto.BrandResp]{Page: 1, Size: 20, Items: []*productdto.BrandResp{{ID: "b1", Name: "示例品牌", Slug: "demo"}}}, "req_path": "/admin/product-brands",

		"Buttons": map[string]any{"product.brand_update": true},
		"Err":     "",
	}))
	for _, want := range []string{`data-write-url="/api/product/brand/seo-score"`, `data-write-handler="product.seo"`} {
		if !strings.Contains(brands, want) {
			t.Fatalf("品牌页应包含 %q，实际输出：%s", want, brands)
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
		Attributes: []*productdto.AttributeResp{attr}, Categories: []*productdto.CategoryResp{{ID: "c1", Name: "男装"}}, Rating: &productdto.RatingResp{},
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
