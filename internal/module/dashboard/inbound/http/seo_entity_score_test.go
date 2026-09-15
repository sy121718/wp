package dashboardhttp

// seo_entity_score_test.go — 商品 / 分类 / 品牌评分接线的测试（审计 SEO-016 / SEO-018）。
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
	sv := entityScoreView(in, res, dups)
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
	sv := entityScoreView(in, res, []scoring.TitleEntry{{Title: "纯棉 T 恤", Page: "/tee-b"}})
	body := renderAdminTemplate(t, "fragments/seo_score", gin.H{"Score": sv})
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
		gin.H{"Score": scoreView{Empty: "读不到这个商品，无法评分"}})
	if !strings.Contains(body, "读不到这个商品，无法评分") {
		t.Fatalf("空态应显示传入的文案，实际输出：%s", body)
	}
}

// TestProductScorePanelsAreWiredIntoAdminTemplates 三个后台页面的评分入口与结果容器。
func TestProductScorePanelsAreWiredIntoAdminTemplates(t *testing.T) {
	products := renderAdminTemplate(t, "admin/products.html", articleLayoutData(gin.H{
		"title": "商品", "menu": "products",
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"WarehouseOptions": []gin.H{}, "Products": []gin.H{productRowForRender()}, "Err": "",
	}))
	for _, want := range []string{
		"hx-post=\"/admin/products/seo-score\"", "id=\"product-seo-score-p1\"",
		"name=\"productId\" value=\"p1\"",
	} {
		if !strings.Contains(products, want) {
			t.Fatalf("商品页应包含 %q，实际输出：%s", want, products)
		}
	}

	categories := renderAdminTemplate(t, "admin/product_categories.html", articleLayoutData(gin.H{
		"title": "商品分类", "menu": "product-categories",
		"Projects": []gin.H{}, "SelectedProject": "proj-1", "Options": []gin.H{},
		"Categories": []gin.H{{"ID": "c1", "Name": "男装", "Slug": "men", "Label": "男装", "Sort": 0,
			"SEOTitle": "", "SEODescription": "", "Description": "", "Image": "", "ParentID": ""}},
		"Err": "",
	}))
	for _, want := range []string{"hx-post=\"/admin/product-categories/seo-score\"", "id=\"category-seo-score-c1\""} {
		if !strings.Contains(categories, want) {
			t.Fatalf("分类页应包含 %q，实际输出：%s", want, categories)
		}
	}

	brands := renderAdminTemplate(t, "admin/product_brands.html", articleLayoutData(gin.H{
		"title": "商品品牌", "menu": "product-brands",
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"Brands": []gin.H{{"ID": "b1", "Name": "示例品牌", "Slug": "demo", "Sort": 0,
			"Logo": "", "SEOTitle": "", "SEODescription": "", "Description": ""}},
		"Err": "",
	}))
	for _, want := range []string{"hx-post=\"/admin/product-brands/seo-score\"", "id=\"brand-seo-score-b1\""} {
		if !strings.Contains(brands, want) {
			t.Fatalf("品牌页应包含 %q，实际输出：%s", want, brands)
		}
	}
}

// productRowForRender 商品列表行数据（键集合与 ProductsPage 组装的 row 一致）。
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
