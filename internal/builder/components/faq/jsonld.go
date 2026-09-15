package faq

import (
	"encoding/json"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// jsonld.go — 常见问题的结构化数据（审计 SEO-006：FAQPage 只输出类型壳）。
//
// 三条取舍：
//
//  1. **与可见问答同源**：可见列表（faq.jet 遍历 BuildView 的结果）与 mainEntity
//     都从同一份 Props.Items 来，不存在第二份问答数据 —— 两处各写一份的结果是
//     「页面上改了答案、结构化数据里还是旧的」这种看不见的分歧。
//
//  2. **注入形态与 seo_head 一致**：同样的 <script type="application/ld+json"> 包裹 +
//     encoding/json 序列化。json.Marshal 会把 < > & 转成 < 等转义序列，
//     因此答案里就算写了 </script> 也逃不出字符串（不需要再手工过滤）。
//
//  3. **答案是纯文本**：可见侧答案经 RichTextHTML 白名单清洗成 HTML 片段，
//     富摘要要的是文字 —— 直接塞 HTML 会被当成答案正文的一部分显示出来。
//
// 为什么不放在页面 <head> 的 JSON-LD 里：head 的输入只有页面设置（internal/builder/
// seo_head.go），拿不到组件 AST；把数据源放在组件内可以让问答与可见项天然同源。
// 因此 seo_head 侧在 schemaType=faq 时不再输出空的 FAQPage 壳（见 seo_head.go）。

// richTextBlockBreakRe 富文本里的块级/换行标签。
//
// 结构化数据的答案要的是可读文本，而 core.StripRichTags 只取文本节点、不补分隔：
// 「第一段</p><p>第二段」会被拼成「第一段第二段」。这里先把块级边界折成空格。
// 正则用 raw string 写：Go 双引号字符串里的 \b 是退格字符（不是单词边界），
// 写成普通字符串会让正则静默失配 —— 表现是段落被粘成一段，而没有任何报错。
var richTextBlockBreakRe = regexp.MustCompile(`(?i)</?(p|div|li|blockquote|pre|h[1-6]|br|figure|figcaption)\b[^>]*>`)

// faqJSONLDOpen / faqJSONLDClose 结构化数据的 <script> 外壳（与 seo_head.go 的输出逐字一致）。
const (
	faqJSONLDOpen  = `<script type="application/ld+json">`
	faqJSONLDClose = "</script>"
)

// FAQPageJSONLD 生成 FAQPage 结构化数据片段（含 <script> 外壳）。
//
// 无有效问答（空条目、问题或答案为空）时返回空串：宁可没有结构化数据，
// 也不要一份 mainEntity 为空的 FAQPage —— 那是 Search Console 里的无效数据。
func FAQPageJSONLD(p *Props) string {
	if p == nil {
		return ""
	}
	entity := mainEntityOf(p)
	if len(entity) == 0 {
		return ""
	}
	doc := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "FAQPage",
		"mainEntity": entity,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		// json.Marshal 对 map/string 不会失败；真失败说明文档结构写错了，
		// 此时静默返回空串会让结构化数据永远缺失而无人察觉，故直接 panic 暴露在构建期。
		panic("faq 结构化数据序列化失败: " + err.Error())
	}
	return faqJSONLDOpen + string(b) + faqJSONLDClose
}

// mainEntityOf 由问答属性构造 Question 数组（顺序与可见列表一致）。
func mainEntityOf(p *Props) []map[string]any {
	out := make([]map[string]any, 0, len(p.Items))
	for _, it := range p.Items {
		question := strings.TrimSpace(it.Question)
		answer := plainAnswer(it.Answer)
		if question == "" || answer == "" {
			continue
		}
		out = append(out, map[string]any{
			"@type": "Question",
			"name":  question,
			"acceptedAnswer": map[string]any{
				"@type": "Answer",
				"text":  answer,
			},
		})
	}
	return out
}

// plainAnswer 答案纯文本化：与可见答案走同一条富文本管线（RichTextHTML 白名单清洗），
// 再剥标签、把块级边界折成空格、归一空白。
//
// 复用同一条清洗链是刻意的：可见答案里进不去的东西（脚本、内联事件属性）同样进不了
// 结构化数据，不必在这里再维护第二套白名单。
func plainAnswer(src string) string {
	rich := core.RichTextHTML(src)
	if rich == "" {
		return ""
	}
	spaced := richTextBlockBreakRe.ReplaceAllString(rich, " ")
	return strings.Join(strings.Fields(core.StripRichTags(spaced)), " ")
}
