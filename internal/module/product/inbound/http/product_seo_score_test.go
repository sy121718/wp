package producthttp

// 商品页面挂载前端 SEO 控件与媒体控件的模板契约。

import (
	json "go_wp/pkg/jsonx"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// TestProductScorePanelsAreWiredIntoAdminTemplates 三个后台页面的评分入口与结果容器。
func TestProductScorePanelsAreWiredIntoAdminTemplates(t *testing.T) {
	// SEO 检查跟着商品走：它已从列表页的折叠区搬进**商品详情页**
	// （评分/变体/SEO 都是某个商品的属性：SEO 面板在**编辑页**，详情页只读，见 admin-ui-logic §1）。
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
	// 抽屉结构预置在前端 template 中，打开后请求 JSON 事实。
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

		// SEO 按钮在每行的编辑抽屉里，而抽屉由权限决定显隐 —— 不给权限时整块不渲染。
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
	// 单测里的取词函数：与「i18n 未初始化」时的真实链路一致 —— 返回调用点给的中文兜底。
	// 展示文案的取词函数在真实请求里由 shell.TranslateFor(c) 提供（页面路由组的中间件）。
	return func(key, fallback string) string {
		if fallback != "" {
			return fallback
		}
		return key
	}
}

// productRowForRender 商品行数据的渲染用例夹具（详情页 / 编辑页 / 捆绑区块 / 评分片段共用）。
//
// 直接使用 dto.ProductResp 业务对象，与生产页面读取的数据契约相同。
// 模板读 `p.X` 时 struct 缺字段会**渲染期报错**（map 缺键只是 nil），所以这个夹具同时充当
// 「模板 ↔ 结构体」的渲染守卫；字段级双向对账见 product_row_template_test.go。
func productRowForRender() *productdto.ProductResp {
	attr := &productdto.AttributeResp{ID: "a1", Key: "color", Name: "颜色", IsVariation: true, Values: []productdto.AttributeValueResp{}}
	return &productdto.ProductResp{
		ID: "p1", ProjectID: "p1", Name: "测试商品", Slug: "tee", Status: "published", PriceMin: 59, PriceMax: 99, VariantCount: 1,
		Variants:   []*productdto.VariantResp{{ID: "v1", SKUCode: "SZ_TEE_001", Price: 59, Enabled: true, StockTotal: 10, OptionValues: json.RawMessage("{}")}},
		Attributes: []*productdto.AttributeResp{attr}, Categories: []*productdto.CategoryResp{{ID: "c1", Name: "男装"}}, Rating: &productdto.RatingResp{},
	}
}

// TestProductEditRendersMediaGallery 图集字段必须是多图控件，而不是裸 textarea。
//
// 为什么值得钉：图集原来是「一行一个地址」的 textarea —— 运营看不到图长什么样、也没有
// 从媒体库挑的入口（上传与选择只走媒体库，控件自己不持有文件输入）。换控件时最容易出的
// 事故是「后端协议被顺手改掉」：本用例同时断言两个字段名仍是 images / imageAlts，
// 值仍走隐藏域（后端的「每行一个」形态不变）。
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
		// 主图：契约里一直有（CreateReq/UpdateReq 的 defaultImage），页面上曾经完全没有
		// 对应控件 —— 运营只能靠导入或接口设它。这条断言防的是「再次掉回契约有、UI 缺」。
		`name="defaultImage"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("商品编辑页的图集控件缺少 %q", want)
		}
	}
	if strings.Contains(out, `id="product-edit-images"`) {
		t.Error("图集仍是裸 textarea：应换成多图控件")
	}
	// 添加入口只留网格末尾的「＋」框：标题行那个同动作的「添加图片」按钮已删
	//（两个入口分处标题行与网格，看着像两件事）。负向断言防它被加回来。
	if n, box := strings.Count(out, "data-gallery-add"), strings.Count(out, "data-gallery-addbox"); n != box {
		t.Errorf("图集的添加入口应只有「＋」框一个：data-gallery-add 出现 %d 次、data-gallery-addbox 出现 %d 次", n, box)
	}
	if strings.Contains(out, "media-pair") {
		t.Error("主图与图集应各占一整行：并排后屏幕上并排两个虚线「＋」框，像同一个控件的两个格子")
	}
}

// TestTaxonomyFormsUseMediaField 分类图与品牌 logo 必须走媒体字段（缩略图 + 媒体库 + 清除），
// 而不是裸文本框。
//
// 判据是「入口只有一个」：控件自己不持有文件输入，唯一的上传/选择入口是媒体库弹窗。
// 裸文本框的失效模式不是崩，而是运营只能手敲 URL —— 敲错要等发布后才发现。
//
// 读模板源而不是渲染：这两个表单是**片段**（被列表页 include），渲染它们要凑齐一整套
// 由 handler 装配的 data，而这里要问的问题（「这个字段是什么控件」）本来就是源文件的事实。
func TestTaxonomyFormsUseMediaField(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		field string
	}{
		{"分类图", "../../../../../internal/templates/admin/product/product_category_form.html", "image"},
		// 品牌的编辑抽屉是列表页里的内联模板（不再有独立片段文件），logo 控件就在那里。
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
		// 反向：不该再有裸文件输入（上传只走媒体库）。
		if strings.Contains(src, `type="file"`) {
			t.Errorf("%s 出现了文件输入：上传与选择只该走媒体库", tc.name)
		}
	}
}
