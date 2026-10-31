package contentservice

import "testing"

func TestMergeSEOFields(t *testing.T) {
	data := map[string]any{
		"title":          "新标题",
		"excerpt":        "新摘要",
		"seoTitle":       "旧 SEO 标题",
		"seoDescription": "旧 SEO 描述",
	}
	mergeSEOFields("article", data)
	if data["seoTitle"] != "新标题" {
		t.Errorf("seoTitle 应归一为新标题，实际 %v", data["seoTitle"])
	}
	if data["seoDescription"] != "新摘要" {
		t.Errorf("seoDescription 应归一为新摘要，实际 %v", data["seoDescription"])
	}
}

func TestMergeSEOFieldsKeepsValueWhenSourceMissing(t *testing.T) {
	data := map[string]any{"seoTitle": "旧 SEO 标题"}
	mergeSEOFields("article", data)
	if data["seoTitle"] != "旧 SEO 标题" {
		t.Errorf("没有 title 时不该改写 seoTitle，实际 %v", data["seoTitle"])
	}
	if _, ok := data["seoDescription"]; ok {
		t.Error("没有 excerpt 时不该凭空造出 seoDescription")
	}
}

func TestMergeSEOFieldsIgnoresOtherEntityTypes(t *testing.T) {
	data := map[string]any{"title": "标题", "seoTitle": "旧值"}
	mergeSEOFields("product", data)
	if data["seoTitle"] != "旧值" {
		t.Errorf("非文章实体不该被归一，实际 %v", data["seoTitle"])
	}
}

func TestMergeSEOFieldsNilData(t *testing.T) {
	mergeSEOFields("article", nil)
}
