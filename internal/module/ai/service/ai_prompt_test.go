package aiservice

// ai_prompt_test.go — 稳定前缀纪律（docs/16 §3）的静态断言。
//
// 这一批的风险不是「功能不对」，而是**每轮多花一次整段前缀的钱**：
// 往 system 里塞一个时间戳、把两段对调、或者每次请求重新渲染规则文本，
// 都不会有任何报错，也不会让任何页面显示异常 —— 只让账单变贵、缓存命中率掉下去。
// 所以判据必须是「两次构造逐字节相等」，而不是「规则非空」。

import (
	"strings"
	"testing"

	aiprompt "go_wp/internal/module/ai/prompt"
)

// TestSiteRulesAreByteStable 同一进程内两次取规则必须逐字节一致。
func TestSiteRulesAreByteStable(t *testing.T) {
	a := aiprompt.SiteRules()
	b := aiprompt.SiteRules()
	if a != b {
		t.Fatal("SiteRules 两次调用不一致 —— 规则里混进了会变的内容")
	}
	if strings.TrimSpace(a) == "" {
		t.Fatal("常驻规则为空：模型会按自己的常识回答金额与口径，而那种回答看起来很正常")
	}
	// 判「有没有会变的内容」不能靠关键词：规则文档里**本来就**会写日期示例
	//（`from=2026-09-01`，静态、合法）与「不要写时效性描述」这类元讨论 ——
	// 按词判会把这些自己误伤，而真正会变的东西（模板占位符）反倒漏掉。
	// 可判的判据只有一条：**文本里不能再有运行时填充位**。有它说明这段字串是渲染出来的，
	// 那就一定会随请求变化。
	for _, banned := range []string{"{{", "}}", "%s", "%d", "%v"} {
		if strings.Contains(a, banned) {
			t.Errorf("常驻规则里出现运行时填充位 %q —— 说明这段文本是渲染出来的，每轮都会变，前缀缓存必然整段失效", banned)
		}
	}
}

// TestStablePrefixOrderAndStability 前缀的顺序是契约，且两次构造逐字节一致。
func TestStablePrefixOrderAndStability(t *testing.T) {
	const history = "user: 这周卖得最好的是什么"
	first := stablePrefix(history, nil)
	second := stablePrefix(history, nil)
	// 两段 system（规则 + 手册目录）+ 一条 user。目录为空时是两段 —— 两种情况都合法，
	// 所以判据写成「至少两段、最后一段是 user」，而不是写死条数。
	if len(first) < 2 || len(first) != len(second) {
		t.Fatalf("稳定前缀至少要有 system + user 两段，且两次构造条数一致，实得 %d / %d", len(first), len(second))
	}
	// 顺序：system 全部在前 —— 任何一个排在 user 之后就不是「前缀」，缓存命中不了。
	last := first[len(first)-1]
	if last.Role != roleUser {
		t.Errorf("最后一条必须是 user，实得 %q", last.Role)
	}
	for i := 0; i < len(first)-1; i++ {
		if first[i].Role != roleSystem {
			t.Errorf("第 %d 条应为 system（所有 system 必须在 user 之前），实得 %q", i, first[i].Role)
		}
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("第 %d 条两次构造不一致：%q/%q vs %q/%q",
				i, first[i].Role, first[i].Content, second[i].Role, second[i].Content)
		}
	}
	if first[0].Content != aiprompt.SiteRules() {
		t.Error("第一段 system 必须就是常驻规则原文（不要在拼接时加包裹文本，那会让规则改动的影响面翻倍）")
	}
	if last.Content != history {
		t.Error("最后一条必须是「历史 + 本轮输入」原文，不得改写")
	}
}

// TestStablePrefixExcludesModelName 前缀里不得出现模型名 / 会话 id 这类每会话变的东西。
//
// 模型在会话中途是可以换的（换模型本来就会作废前缀缓存，docs/16 §3 要求把这件事单独
// 归因）；但如果模型名被拼进前缀，换模型的影响会被误记成「缓存过期」。
func TestStablePrefixExcludesModelName(t *testing.T) {
	msgs := stablePrefix("user: hi", nil)
	joined := msgs[0].Content
	for _, banned := range []string{"gpt-", "claude-", "deepseek-", "session_id", "sessionId"} {
		if strings.Contains(joined, banned) {
			t.Errorf("常驻规则里出现 %q —— 会话 / 模型相关的内容不该进前缀", banned)
		}
	}
}
