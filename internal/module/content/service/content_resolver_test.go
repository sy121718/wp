package contentservice

import "testing"

// mergeSEOFields 的判据（2026-09-30 字段合并）：文章的 SEO 标题 / 描述就是标题 / 摘要，
// 遗留的旧值必须被覆盖 —— 这条一旦失效，未重新保存过的老文章会带着旧 SEO 标题发布，
// 而编辑页上根本看不到那个值（最难查的一类不一致）。
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

// 缺正文字段时不动：字段白名单里仍留着 seoTitle，旧文档可能绑着它，
// 这里不能把值改写成 nil（等于把那个绑定悄悄挖空）。
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

// 别的实体类型不受影响（归一目前只对文章；商品 / 分类 / 品牌的映射在各自模块里）。
func TestMergeSEOFieldsIgnoresOtherEntityTypes(t *testing.T) {
	data := map[string]any{"title": "标题", "seoTitle": "旧值"}
	mergeSEOFields("product", data)
	if data["seoTitle"] != "旧值" {
		t.Errorf("非文章实体不该被归一，实际 %v", data["seoTitle"])
	}
}

// 空 data 不 panic。
func TestMergeSEOFieldsNilData(t *testing.T) {
	mergeSEOFields("article", nil)
}
