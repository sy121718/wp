package faq

import (
	"encoding/json"
	"strings"
	"testing"
)

// jsonld_test.go — 常见问题结构化数据（审计 SEO-006）。
//
// 断言三件事：
//  1. mainEntity 与可见问答同源（同一条数、同一顺序、同一文本）；
//  2. 注入形态可解析（JSON 合法、@type=FAQPage），且答案里的尖括号逃不出脚本块；
//  3. 无有效问答时不输出（不产生空壳 FAQPage）。

// parseFAQJSONLD 解析结构化数据片段（去掉 script 外壳后按 JSON 解析）。
func parseFAQJSONLD(t *testing.T, out string) map[string]any {
	t.Helper()
	body := strings.TrimPrefix(out, faqJSONLDOpen)
	body = strings.TrimSuffix(body, faqJSONLDClose)
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("结构化数据不是合法 JSON：%v；原文：%s", err, out)
	}
	return doc
}

// answerTextOf 取第 i 条 mainEntity 的答案文本。
func answerTextOf(t *testing.T, out string, i int) string {
	t.Helper()
	entity, _ := parseFAQJSONLD(t, out)["mainEntity"].([]any)
	if i >= len(entity) {
		t.Fatalf("mainEntity 只有 %d 条，取不到第 %d 条：%s", len(entity), i+1, out)
	}
	item, _ := entity[i].(map[string]any)
	answer, _ := item["acceptedAnswer"].(map[string]any)
	if answer == nil {
		t.Fatalf("第 %d 条缺少 acceptedAnswer：%#v", i+1, item)
	}
	text, _ := answer["text"].(string)
	return text
}

// TestFAQPageJSONLDMatchesVisibleItems mainEntity 与可见问答逐条对齐。
func TestFAQPageJSONLDMatchesVisibleItems(t *testing.T) {
	p := &Props{Items: []FaqItem{
		{Question: "问题一 & 特殊", Answer: "<p>答案 <strong>加粗</strong> 内容</p>", Open: true},
		{Question: "问题二", Answer: "纯文本答案 1 < 2 & 更多" + "\n\n" + "第二段"},
	}}
	out := FAQPageJSONLD(p)
	if out == "" {
		t.Fatal("有问答时应输出结构化数据")
	}
	doc := parseFAQJSONLD(t, out)
	if doc["@type"] != "FAQPage" {
		t.Errorf("@type = %v，期望 FAQPage", doc["@type"])
	}
	if doc["@context"] != "https://schema.org" {
		t.Errorf("@context = %v", doc["@context"])
	}
	entity, ok := doc["mainEntity"].([]any)
	if !ok || len(entity) != len(p.Items) {
		t.Fatalf("mainEntity 条数应为 %d，实际 %#v", len(p.Items), doc["mainEntity"])
	}
	// 与可见项同源：问题文本一致（可见侧由 Jet 转义输出，源文本是同一份）。
	first, _ := entity[0].(map[string]any)
	if first["@type"] != "Question" || first["name"] != "问题一 & 特殊" {
		t.Errorf("第 1 条 Question 不符：%#v", first)
	}
	// 答案是纯文本：标签被剥掉，实体解码回原字符。
	if got := answerTextOf(t, out, 0); got != "答案 加粗 内容" {
		t.Errorf("答案纯文本化结果不符：%q", got)
	}
	if got := answerTextOf(t, out, 1); got != "纯文本答案 1 < 2 & 更多 第二段" {
		t.Errorf("多段答案应折成空格分隔：%q", got)
	}
}

// TestFAQPageJSONLDVisibleAndStructuredAgree 可见渲染与结构化数据来自同一份 Props。
func TestFAQPageJSONLDVisibleAndStructuredAgree(t *testing.T) {
	p := &Props{Items: []FaqItem{{Question: "唯一问题", Answer: "<p>唯一答案</p>"}}}
	view := BuildView(p)
	if len(view.Items) != 1 || view.Items[0].Question != "唯一问题" {
		t.Fatalf("可见视图与预期不符：%#v", view.Items)
	}
	entity, _ := parseFAQJSONLD(t, FAQPageJSONLD(p))["mainEntity"].([]any)
	first, _ := entity[0].(map[string]any)
	if first["name"] != view.Items[0].Question {
		t.Errorf("结构化数据问题 %v 与可见项 %v 不同源", first["name"], view.Items[0].Question)
	}
}

// TestFAQPageJSONLDScriptTagCannotEscape 答案里的尖括号逃不出脚本块。
//
// 两道防线：可见侧同一条富文本管线先把标签剥掉（script 标签根本进不来），
// 剩下的 "<" 由 JSON 序列化转成 <（截不断 script 元素）。
func TestFAQPageJSONLDScriptTagCannotEscape(t *testing.T) {
	p := &Props{Items: []FaqItem{
		{Question: "注入尝试", Answer: "危险 </script><script>alert(1)</script> 内容"},
		{Question: "尖括号文本", Answer: "1 < 2 与 & 更多"},
	}}
	out := FAQPageJSONLD(p)
	if got := strings.Count(out, "<script"); got != 1 {
		t.Fatalf("产物里的 script 标签应只有 JSON-LD 外壳本身，实际 %d 个：%s", got, out)
	}
	if got := strings.Count(out, faqJSONLDClose); got != 1 {
		t.Fatalf("脚本外壳应只出现一次闭合标签，实际 %d 次：%s", got, out)
	}
	if got := answerTextOf(t, out, 0); got != "危险 alert(1) 内容" {
		t.Errorf("script 标签应被白名单剥掉、只留文本：%q", got)
	}
	if got := answerTextOf(t, out, 1); !strings.Contains(got, "<") {
		t.Errorf("文本里的尖括号应保留在 JSON 字符串里：%q", got)
	}
	if !strings.Contains(out, `<`) {
		t.Errorf("尖括号应在 JSON 层转义：%s", out)
	}
}

// TestFAQPageJSONLDNoOutputWithoutItems 空/无效问答不产出结构化数据。
func TestFAQPageJSONLDNoOutputWithoutItems(t *testing.T) {
	cases := map[string]*Props{
		"nil":   nil,
		"空条目":   {},
		"缺答案":   {Items: []FaqItem{{Question: "问题"}}},
		"缺问题":   {Items: []FaqItem{{Answer: "答案"}}},
		"问题为空白": {Items: []FaqItem{{Question: "   ", Answer: "答案"}}},
	}
	for name, p := range cases {
		if out := FAQPageJSONLD(p); out != "" {
			t.Errorf("%s：应输出空串，实际 %s", name, out)
		}
	}
}
