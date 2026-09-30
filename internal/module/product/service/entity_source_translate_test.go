package productservice

import (
	"context"
	"testing"

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
