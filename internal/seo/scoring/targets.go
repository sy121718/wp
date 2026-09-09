package scoring

// targets.go — 检查项 → 改进位置映射（前端点击建议后跳转到对应设置/组件）。

// checkTargets 检查项 Key → 定位目标：
//
//	settings.seo.<field>  页面设置里的 SEO 字段
//	node:<type>           画布中第一个该类型组件（如 node:heading 定位 H1）
var checkTargets = map[string]string{
	"title_present":            "settings.seo.title",
	"title_length":             "settings.seo.title",
	"title_keyword_first_half": "settings.seo.title",
	"meta_length":              "settings.seo.description",
	"meta_has_cta":             "settings.seo.description",
	"single_h1":                "node:heading",
	"h1_has_keyword":           "node:heading",
	"heading_hierarchy":        "node:heading",
	"content_length":           "node:text",
	"paragraph_length":         "node:text",
	"sentence_length":          "node:text",
	"keyword_density":          "settings.seo.focusKeyword",
	"keyword_positions":        "settings.seo.focusKeyword",
	"secondary_keywords":       "settings.seo.secondaryKeywords",
	"internal_link_count":      "node:button",
	"external_links":           "node:button",
	"anchor_descriptive":       "node:button",
	"alt_coverage":             "node:image",
	"image_weight":             "node:image",
	"image_format":             "node:image",
	"url_clean":                "settings.seo.canonical",
	"canonical":                "settings.seo.canonical",
	"schema_present":           "settings.seo.schemaType",
}

// TargetOf 返回检查项的定位目标（无映射返回空串）。
func TargetOf(key string) string { return checkTargets[key] }
