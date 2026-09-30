package productservice

import (
	"context"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
)

// 分类 / 品牌的 SEO 字段与名称、描述合并（2026-09-30）：读侧必须给出合并后的值 ——
// 库里可能还留着编辑者过去写过的 seo_title，它一旦仍然是取值来源，
// 「改了分类名、meta 标题没变」这类故障会重新出现，且页面上看不到原因。
func TestCategoryAndBrandSEOFieldsFollowMergedSource(t *testing.T) {
	s := &Service{}
	ctx := context.Background()

	category := &productmodel.ProductCategoryEntity{
		Name: "夏季", Description: "<p>透气亲肤</p>",
		SEOTitle: "旧 SEO 标题", SEODescription: "旧 SEO 描述",
	}
	values := s.categoryValues(ctx, "", category)
	if values["seoTitle"] != "夏季" {
		t.Errorf("分类 seoTitle 应等于分类名，实际 %q", values["seoTitle"])
	}
	// 描述原样透出富文本：去标签在 presentation 的 seoDescriptionText 一处做
	//（两处各做一遍必然分叉）。
	if values["seoDescription"] != "<p>透气亲肤</p>" {
		t.Errorf("分类 seoDescription 应取分类描述，实际 %q", values["seoDescription"])
	}

	brand := &productmodel.ProductBrandEntity{
		Name: "山野", Description: "户外品牌",
		SEOTitle: "旧品牌 SEO 标题", SEODescription: "旧品牌 SEO 描述",
	}
	brandValues := s.brandValues(ctx, "", brand)
	if brandValues["seoTitle"] != "山野" {
		t.Errorf("品牌 seoTitle 应等于品牌名，实际 %q", brandValues["seoTitle"])
	}
	if brandValues["seoDescription"] != "户外品牌" {
		t.Errorf("品牌 seoDescription 应取品牌描述，实际 %q", brandValues["seoDescription"])
	}
}

// applySEOAliases 的判据（2026-09-30 字段合并）：别名取源字段的**当前值** ——
// 调用点放在取词之后，所以这里用「源字段已被替换成译文」的输入模拟多语言构建，
// 别名必须跟着变成译文（否则英文站点上 meta 标题与页面标题是两种语言）。
func TestApplySEOAliases(t *testing.T) {
	batches := []map[string]string{{
		"name": "Summer Shirt", "description": "Breathable",
		"seoTitle": "旧标题", "seoDescription": "旧描述",
	}}
	applySEOAliases(productcontract.EntityTypeCategory, batches)
	if batches[0]["seoTitle"] != "Summer Shirt" {
		t.Errorf("分类 seoTitle 应同步为 name，实际 %q", batches[0]["seoTitle"])
	}
	if batches[0]["seoDescription"] != "Breathable" {
		t.Errorf("分类 seoDescription 应同步为 description，实际 %q", batches[0]["seoDescription"])
	}
}

// 商品的描述别名是 subtitle（不是 description —— 后者是富文本正文）。
func TestApplySEOAliasesProductUsesSubtitle(t *testing.T) {
	batches := []map[string]string{{
		"name": "衬衫", "subtitle": "轻薄透气", "description": "<p>正文</p>",
	}}
	applySEOAliases(productcontract.EntityTypeProduct, batches)
	if batches[0]["seoTitle"] != "衬衫" {
		t.Errorf("商品 seoTitle 应同步为 name，实际 %q", batches[0]["seoTitle"])
	}
	if batches[0]["seoDescription"] != "轻薄透气" {
		t.Errorf("商品 seoDescription 应同步为 subtitle，实际 %q", batches[0]["seoDescription"])
	}
}

// 源字段缺席时保持原值：旧模板可能只绑了别名，不该在这里被清成空串。
func TestApplySEOAliasesKeepsValueWithoutSource(t *testing.T) {
	batches := []map[string]string{{"seoTitle": "旧值"}}
	applySEOAliases(productcontract.EntityTypeCategory, batches)
	if batches[0]["seoTitle"] != "旧值" {
		t.Errorf("没有 name 时不该改写 seoTitle，实际 %q", batches[0]["seoTitle"])
	}
}

// 未登记别名的实体类型（标签 / 属性组）不受影响。
func TestApplySEOAliasesIgnoresOtherTypes(t *testing.T) {
	batches := []map[string]string{{"name": "标签", "seoTitle": "旧值"}}
	applySEOAliases(productcontract.EntityTypeTag, batches)
	if batches[0]["seoTitle"] != "旧值" {
		t.Errorf("标签类型不该被同步，实际 %q", batches[0]["seoTitle"])
	}
}
