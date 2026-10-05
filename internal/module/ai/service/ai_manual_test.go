package aiservice_test

// ai_manual_test.go — 领域手册的可达性断言（P1 的闸门）。
//
// P1 的闸门原文是「AI 在不知道任何 schema 的情况下，能把『本店这周卖得最好的是什么』
// 定位到正确模块与工具」。那件事没法直接断言（要跑一次真实的模型），但它的**必要前提**
// 可以逐条钉住：
//   1. 目录里列出的每个名字都真的取得到（否则模型按目录去取，取到空手）；
//   2. 每个手册文件都在目录里（否则手册存在但模型不知道它存在）；
//   3. 与闸门对应的那条链路是闭环的 —— 「卖得最好的商品」这句话里出现的关键词，
//      必须能在一篇手册里被引到具体工具名上。
//
// 这三条都失败得很安静：模型不会报错，它只会用常识回答一个数字。

import (
	"strings"
	"testing"

	aiprompt "go_wp/internal/module/ai/prompt"
)

// TestManualCatalogMatchesFiles 目录与文件双向一致。
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

// TestManualRejectsBadNames 取不存在 / 形状不对的名字必须回 false，不能回空串当成功。
//
// 回空串会让模型以为「这个领域没有口径」，于是按常识回答 —— 那正是 guide 要解决的问题。
func TestManualRejectsBadNames(t *testing.T) {
	for _, bad := range []string{"", "  ", "nope", "../site_rules", "manual/sales", "sales.md", "sales/../customers"} {
		if _, ok := aiprompt.Manual(bad); ok {
			t.Errorf("%q 不该被当成有效手册名（路径穿越与空名字都要挡住）", bad)
		}
	}
}

// TestSalesManualPointsAtTheTool P1 闸门那条链路的闭环检查。
//
// 「本店这周卖得最好的是什么」要想被答对，模型得先知道：
//   · 这件事属于哪个领域（目录里有 sales）；
//   · 那个领域里该用哪个工具（sales 手册里点名 orders_top_products）。
//
// 断言工具名出现在手册里，而不是断言某句话 —— 手册措辞可以改，
// 但「手册必须把领域引到具体工具上」这条不能破，否则手册就只是一篇散文。
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
	// 口径里最容易答错的两条必须写出来（金额单位、哪些订单算消费）。
	for _, want := range []string{"分", "paid"} {
		if !strings.Contains(text, want) {
			t.Errorf("sales 手册缺少关键口径 %q", want)
		}
	}
	// 目录要让模型知道「卖」这件事归 sales。
	if catalog := aiprompt.ManualCatalog(); !strings.Contains(catalog, "sales") {
		t.Error("目录里没有 sales，模型不会去取它")
	}
}

// TestManualCatalogIsByteStable 目录进稳定前缀，所以它也必须字节稳定。
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
