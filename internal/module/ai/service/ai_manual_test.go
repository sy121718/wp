package aiservice_test

import (
	"strings"
	"testing"

	aiprompt "go_wp/internal/module/ai/prompt"
)

func TestManualCatalogMatchesFiles(t *testing.T) {
	topics := aiprompt.ManualTopics()
	if len(topics) == 0 {
		t.Fatal("没有任何手册 —— guide 工具会变成一个永远报错的工具")
	}
	for _, topic := range topics {
		text, ok := aiprompt.Manual(topic.Name)
		if !ok || strings.TrimSpace(text) == "" {
			t.Errorf("目录里列了 %q，但取不到内容（模型按目录去取会空手而归）", topic.Name)
		}
		if topic.Title == "" || topic.Title == topic.Name {
			t.Errorf("手册 %q 没有 `# 标题` 首行（目录里会露出英文文件名），实得 %q", topic.Name, topic.Title)
		}
	}
	catalog := aiprompt.ManualCatalog()
	for _, topic := range topics {
		if !strings.Contains(catalog, topic.Name) {
			t.Errorf("手册 %q 不在目录里（模型不知道它存在）", topic.Name)
		}
	}
}

func TestManualRejectsBadNames(t *testing.T) {
	for _, bad := range []string{"", "  ", "nope", "../site_rules", "manual/sales", "sales.md", "sales/../customers"} {
		if _, ok := aiprompt.Manual(bad); ok {
			t.Errorf("%q 不该被当成有效手册名（路径穿越与空名字都要挡住）", bad)
		}
	}
}

func TestSalesManualPointsAtTheTool(t *testing.T) {
	text, ok := aiprompt.Manual("sales")
	if !ok {
		t.Fatal("没有 sales 手册")
	}
	for _, want := range []string{"orders_top_products", "orders_summary"} {
		if !strings.Contains(text, want) {
			t.Errorf("sales 手册没有提到工具 %q —— 模型读完手册仍不知道该调什么", want)
		}
	}

	for _, want := range []string{"分", "paid"} {
		if !strings.Contains(text, want) {
			t.Errorf("sales 手册缺少关键口径 %q", want)
		}
	}

	if catalog := aiprompt.ManualCatalog(); !strings.Contains(catalog, "sales") {
		t.Error("目录里没有 sales，模型不会去取它")
	}
}

func TestManualCatalogIsByteStable(t *testing.T) {
	a := aiprompt.ManualCatalog()
	b := aiprompt.ManualCatalog()
	if a != b {
		t.Fatal("手册目录两次生成不一致 —— 它进了稳定前缀，不稳定会让缓存每轮作废")
	}
	if strings.Contains(a, "\t") {
		t.Error("目录里出现制表符：嵌入模板会把它渲染成不可预期的东西")
	}
}
