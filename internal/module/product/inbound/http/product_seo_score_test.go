package producthttp

// product_seo_score_test.go — 商品 / 分类 / 品牌评分接线的测试（审计 SEO-016 / SEO-018）。
//
// 原 dashboard/inbound/http/seo_entity_score_test.go，随商品页搬回本模块。
//
// 三层各钉一段：
//   - 纯逻辑：页面草稿取标题、商品描述去标签、评分视图把冲突页面装进片段数据；
//   - 片段渲染：fragments/seo_score 真的渲染出页型理由与冲突页面清单；
//   - 页面渲染：三个后台页面的评分挂载点（按钮 + 结果容器）确实在 HTML 里 ——
//     端点写好了但页面上没有入口，等于这个能力不存在（SEO-016 的根因就是这种缺口）。
//
// 不碰数据库：这一组全是纯函数与模板渲染，与模块内就近单测的定位一致。

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/seo/scoring"
)

// TestPageDraftTitleExtractsSeoTitle 页面草稿的标题索引取的是 settings.seo.title。
func TestPageDraftTitleExtractsSeoTitle(t *testing.T) {
	doc := json.RawMessage(`{"settings":{"seo":{"title":"  关于我们  "}},"root":[]}`)
	if got := pageDraftTitle(doc); got != "关于我们" {
		t.Fatalf("应取到 trim 后的 settings.seo.title，实际 %q", got)
	}
	if got := pageDraftTitle(json.RawMessage(`{"settings":{}}`)); got != "" {
		t.Fatalf("没有 SEO 标题时应返回空串，实际 %q", got)
	}
	if got := pageDraftTitle(nil); got != "" {
		t.Fatalf("空文档应返回空串，实际 %q", got)
	}
	if got := pageDraftTitle(json.RawMessage(`不是 JSON`)); got != "" {
		t.Fatalf("坏 JSON 应返回空串而不是 panic，实际 %q", got)
	}
}

// TestEntityPlainTextStripsTags 商品描述（JSONB 两种形态）→ 纯文本。
func TestEntityPlainTextStripsTags(t *testing.T) {
	rich := json.RawMessage(`{"html":"<p>纯棉 <strong>透气</strong></p>"}`)
	if got := entityPlainText(rich); got != "纯棉 透气" {
		t.Fatalf("富文本描述应去标签，实际 %q", got)
	}
	plain := json.RawMessage(`"夏季新品"`)
	if got := entityPlainText(plain); got != "夏季新品" {
		t.Fatalf("字符串描述应原样返回，实际 %q", got)
	}
	if got := entityPlainText(nil); got != "" {
		t.Fatalf("空描述应返回空串，实际 %q", got)
	}
}

// TestEntityScoreViewCarriesDuplicatePages 评分视图把冲突页面原样带进片段数据。
func TestEntityScoreViewCarriesDuplicatePages(t *testing.T) {
	in := &scoring.EntityPageInput{Kind: scoring.KindProduct, Name: "纯棉 T 恤", Slug: "tee"}
	res := scoring.ScoreEntityPage(in)
	dups := []scoring.TitleEntry{
		{Title: "纯棉 T 恤", Page: "/tee-b"},
		{Title: "纯棉 T 恤", Page: "/tee-c"},
	}
	sv := entityScoreView(testTranslateFunc(), in, res, dups)
	if len(sv.Duplicates) != 2 || sv.Duplicates[0] != "/tee-b" || sv.Duplicates[1] != "/tee-c" {
		t.Fatalf("冲突页面应逐个列出，实际 %+v", sv.Duplicates)
	}
	if !strings.Contains(sv.DuplicateNote, "/tee-b") || !strings.Contains(sv.DuplicateNote, "/tee-c") {
		t.Fatalf("冲突说明应列出全部命中页面，实际 %q", sv.DuplicateNote)
	}
	if sv.ProfileType != "product" || sv.ProfileReason == "" {
		t.Fatalf("商品页应回显页型与理由，实际 type=%q reason=%q", sv.ProfileType, sv.ProfileReason)
	}
}

// TestSeoScoreFragmentRendersProfileAndDuplicates 片段渲染出页型回显与冲突清单。
func TestSeoScoreFragmentRendersProfileAndDuplicates(t *testing.T) {
	in := &scoring.EntityPageInput{Kind: scoring.KindProduct, Name: "纯棉 T 恤", Slug: "tee"}
	res := scoring.ScoreEntityPage(in)
	sv := entityScoreView(testTranslateFunc(), in, res, []scoring.TitleEntry{{Title: "纯棉 T 恤", Page: "/tee-b"}})
	// t 必须给：fragments/seo_score 的 data 是 gin.H（不是 struct），模板用 .["t"] 取词；
	// 缺 t 时 Jet 把取词调用求值成空串（不报错、不 500），下面这几条断言会看不到任何文案。
	body := renderAdminTemplate(t, "fragments/seo_score", gin.H{"Score": sv, "t": testTranslateFunc()})
	for _, want := range []string{
		"SEO 评分（0-100）", "页型权重：product", "标题重复", "/tee-b", "重复的 title（纯棉 T 恤）",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("片段里应出现 %q，实际输出：%s", want, body)
		}
	}
}

// TestSeoScoreFragmentRendersEntityEmptyState 读不到实体时给一句可读的话，不是空白面板。
func TestSeoScoreFragmentRendersEntityEmptyState(t *testing.T) {
	body := renderAdminTemplate(t, "fragments/seo_score",
		gin.H{"Score": scoreView{Empty: "读不到这个商品，无法评分"}, "t": testTranslateFunc()})
	if !strings.Contains(body, "读不到这个商品，无法评分") {
		t.Fatalf("空态应显示传入的文案，实际输出：%s", body)
	}
}

// TestProductScorePanelsAreWiredIntoAdminTemplates 三个后台页面的评分入口与结果容器。
func TestProductScorePanelsAreWiredIntoAdminTemplates(t *testing.T) {
	// SEO 检查跟着商品走：它已从列表页的折叠区搬进**商品详情页**
	// （评分/变体/SEO 都是某个商品的属性：SEO 面板在**编辑页**，详情页只读，见 admin-ui-logic §1）。
	product := productRowForRender()
	product["Subtitle"] = ""
	product["Unit"] = ""
	product["SEOTitle"] = ""
	product["SEODescription"] = ""
	products := renderAdminTemplate(t, "admin/product/product_edit.html", productPageLayoutData(gin.H{
		"title": "编辑商品", "menu": "products",
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"WarehouseOptions": []gin.H{}, "Err": "",
		"HasProduct": true, "ProductID": "p1", "BackURL": "/admin/products",
		"Product":         product,
		"Statuses":        []gin.H{{"Value": "draft", "Label": "草稿", "Selected": true}},
		"AttributeChecks": []gin.H{},
		"ImagesText":      "", "ImageAltsText": "", "WeightText": "", "DefaultPriceText": "",
	}))
	// 商品编辑页只留**抽屉入口**：评测内容（分数容器、隐藏字段、评分按钮）已移进
	// admin/product/entity_seo_drawer.html —— 它们不该再常驻在编辑页上。
	for _, want := range []string{
		`data-drawer-url="/admin/products/seo/drawer?productId=p1`,
		"SEO 评分",
	} {
		if !strings.Contains(products, want) {
			t.Fatalf("商品编辑页应包含 %q，实际输出：%s", want, products)
		}
	}
	if strings.Contains(products, `id="product-seo-score-p1"`) {
		t.Error("评分容器不应再常驻编辑页：它属于抽屉片段")
	}

	categories := renderAdminTemplate(t, "admin/product/product_categories.html", productPageLayoutData(gin.H{
		"title": "商品分类", "menu": "product-categories",
		"Projects": []gin.H{}, "SelectedProject": "proj-1", "Options": []gin.H{},
		"Categories": []gin.H{{"ID": "c1", "Name": "男装", "Slug": "men", "Label": "男装", "Sort": 0,
			"SEOTitle": "", "SEODescription": "", "Description": "", "Image": "", "ParentID": ""}},
		// SEO 按钮在每行的编辑抽屉里，而抽屉由权限决定显隐 —— 不给权限时整块不渲染。
		"PermSet": map[string]any{"product:category_update": true},
		"Err":     "",
	}))
	for _, want := range []string{"hx-post=\"/admin/product-categories/seo-score\"", "id=\"category-seo-score-c1\""} {
		if !strings.Contains(categories, want) {
			t.Fatalf("分类页应包含 %q，实际输出：%s", want, categories)
		}
	}

	brands := renderAdminTemplate(t, "admin/product/product_brands.html", productPageLayoutData(gin.H{
		"title": "商品品牌", "menu": "product-brands",
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"Brands": []gin.H{{"ID": "b1", "Name": "示例品牌", "Slug": "demo", "Sort": 0,
			"Logo": "", "SEOTitle": "", "SEODescription": "", "Description": ""}},
		"PermSet": map[string]any{"product:brand_update": true},
		"Err":     "",
	}))
	for _, want := range []string{"hx-post=\"/admin/product-brands/seo-score\"", "id=\"brand-seo-score-b1\""} {
		if !strings.Contains(brands, want) {
			t.Fatalf("品牌页应包含 %q，实际输出：%s", want, brands)
		}
	}
}

// productRowForRender 商品列表行数据（键集合与 ProductsPage 组装的 row 一致）。
// productRowForRender 商品行数据的渲染用例夹具（评分片段与页面挂载点共用）。
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

func productRowForRender() gin.H {
	attr := &productdto.AttributeResp{ID: "a1", Name: "颜色", IsVariation: true}
	return gin.H{
		"ID": "p1", "Name": "纯棉 T 恤", "Slug": "tee", "Status": "published",
		"PriceMin": "59", "PriceMax": "99", "VariantCount": 1,
		"Variants": []gin.H{{
			"ID": "v1", "SKUCode": "SZ_TEE_001", "Spec": "颜色 白色",
			"Price": "59", "ComparePrice": "—", "CostPrice": "—", "Enabled": true, "StockTotal": 10,
		}},
		"AttributeIDs": []string{"a1"}, "AttributeIDsCSV": "a1",
		"Attributes":          []*productdto.AttributeResp{attr},
		"VariationAttributes": []*productdto.AttributeResp{attr},
		"CategoryIDs":         []string{"c1"},
		"CategoryChecks":      []gin.H{{"ID": "c1", "Label": "男装", "Checked": true}},
		"PrimaryOptions":      []gin.H{{"ID": "c1", "Label": "男装", "Selected": true}},
		"BrandOptions":        []gin.H{{"ID": "", "Label": "（不指定品牌）", "Selected": true}},
		"PrimaryCategoryName": "男装", "BrandName": "—",
		"TagIDs": []string{}, "TagChecks": []gin.H{}, "AutoTags": []gin.H{},
		"Ratings": []gin.H{}, "HasRating": false, "RatingAvg": "0.00", "RatingCount": 0,
	}
}

// TestProductSeoDrawerFragmentRenders 抽屉片段：评分按钮、提交端点与结果容器都在这里。
//
// 与编辑页那条断言是一对：编辑页只剩入口，而入口指向的片段若接错（端点写错、容器 id 与
// hx-target 不一致、缺 csrf 隐藏域），用户点开抽屉会看到一个按不动的按钮 —— 页面测试全绿。
func TestProductSeoDrawerFragmentRenders(t *testing.T) {
	body := renderAdminTemplate(t, "admin/product/entity_seo_drawer.html", productPageLayoutData(gin.H{
		"Score":        scoreView{OK: true, Total: 72, Grade: "C"},
		"ScoreURL":     "/admin/products/seo-score",
		"TargetID":     "product-seo-score-p1",
		"HiddenFields": []seoHiddenField{{"projectId", "proj-1"}, {"productId", "p1"}},
	}))
	for _, want := range []string{
		"data-drawer-fragment",
		`action="/admin/products/seo-score"`, `hx-post="/admin/products/seo-score"`,
		`hx-target="#product-seo-score-p1"`, `id="product-seo-score-p1"`,
		`name="productId" value="p1"`, `name="csrf_token"`,
		"data-drawer-close",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("抽屉片段缺少 %q", want)
		}
	}
	for _, bad := range []string{"<script", "<iframe"} {
		if strings.Contains(body, bad) {
			t.Errorf("抽屉片段出现 %q：drawer.js 会判非法", bad)
		}
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
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"WarehouseOptions": []gin.H{}, "Err": "",
		"HasProduct": true, "ProductID": "p1", "BackURL": "/admin/products",
		"Product":         product,
		"Statuses":        []gin.H{{"Value": "draft", "Label": "草稿", "Selected": true}},
		"AttributeChecks": []gin.H{},
		"ImagesText":      "http://127.0.0.1:8080/storage/372.webp",
		"ImageAltsText":   "示例 alt",
		"WeightText":      "", "DefaultPriceText": "",
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
		{"品牌 logo", "../../../../../internal/templates/admin/product/product_brand_form.html", "logo"},
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
