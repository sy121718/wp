package aiservice

import (
	"strings"
	"testing"

	aiprompt "go_wp/internal/module/ai/prompt"
)

func TestSiteRulesAreByteStable(t *testing.T) {
	a := aiprompt.SiteRules()
	b := aiprompt.SiteRules()
	if a != b {
		t.Fatal("SiteRules 两次调用不一致 —— 规则里混进了会变的内容")
	}
	if strings.TrimSpace(a) == "" {
		t.Fatal("常驻规则为空：模型会按自己的常识回答金额与口径，而那种回答看起来很正常")
	}

	for _, banned := range []string{"{{", "}}", "%s", "%d", "%v"} {
		if strings.Contains(a, banned) {
			t.Errorf("常驻规则里出现运行时填充位 %q —— 说明这段文本是渲染出来的，每轮都会变，前缀缓存必然整段失效", banned)
		}
	}
}

func TestStablePrefixOrderAndStability(t *testing.T) {
	const history = "user: 这周卖得最好的是什么"
	first := stablePrefix(history, nil)
	second := stablePrefix(history, nil)

	if len(first) < 2 || len(first) != len(second) {
		t.Fatalf("稳定前缀至少要有 system + user 两段，且两次构造条数一致，实得 %d / %d", len(first), len(second))
	}

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

func TestStablePrefixExcludesModelName(t *testing.T) {
	msgs := stablePrefix("user: hi", nil)
	joined := msgs[0].Content
	for _, banned := range []string{"gpt-", "claude-", "deepseek-", "session_id", "sessionId"} {
		if strings.Contains(joined, banned) {
			t.Errorf("常驻规则里出现 %q —— 会话 / 模型相关的内容不该进前缀", banned)
		}
	}
}
