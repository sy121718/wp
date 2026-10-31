package productservice

import (
	"context"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
)

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

func TestApplySEOAliasesKeepsValueWithoutSource(t *testing.T) {
	batches := []map[string]string{{"seoTitle": "旧值"}}
	applySEOAliases(productcontract.EntityTypeCategory, batches)
	if batches[0]["seoTitle"] != "旧值" {
		t.Errorf("没有 name 时不该改写 seoTitle，实际 %q", batches[0]["seoTitle"])
	}
}

func TestApplySEOAliasesIgnoresOtherTypes(t *testing.T) {
	batches := []map[string]string{{"name": "标签", "seoTitle": "旧值"}}
	applySEOAliases(productcontract.EntityTypeTag, batches)
	if batches[0]["seoTitle"] != "旧值" {
		t.Errorf("标签类型不该被同步，实际 %q", batches[0]["seoTitle"])
	}
}
