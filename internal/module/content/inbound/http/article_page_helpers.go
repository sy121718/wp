package contenthttp

// article_page_helpers.go — 文章页用到的小工具（搬迁时按来源逐字复制）。
//
// 它们原先散在 dashboard 包的多个文件里（product_pricing_handle.go 的 firstNonEmpty、
// seo_entity_score_input.go 的 contentText / firstNonEmptyString、
// product_translation_data.go 的 hasMarkup、page_translations_handle.go 的
// translationLangOption）。跨包引私有符号不成立，页面搬回本模块后各自持有一份 ——
// 都是几行的纯函数，语义不会分叉。

import "strings"

// firstNonEmpty 取第一个非空字符串（返回值不做 Trim，与 dashboard 版一致 ——
// 调用方拿到的就是原值，判定用的是 Trim 后的结果）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyString 取第一个非空值（返回 Trim 后的结果）。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// hasMarkup 是否含 HTML 标签（与构建期 core.HasRichMarkup 同一判据的轻量版）。
func hasMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

// translationLangOption 工作台语言下拉项。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}
