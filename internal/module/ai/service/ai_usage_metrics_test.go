package aiservice

// ai_usage_metrics_test.go — docs/16 §3.1 的三个验收数字的数据侧断言。
//
// 这一批的风险不在「算错」，而在**把没数据算成 0**：
// 上游没报缓存字段时，命中率是「无从判断」而不是「0%」；
// 前者只是少一个观测，后者是一个严重的信号（前缀每轮都在变）。
// 两者在页面上长得一样的话，最该被看见的那条信息就被抹掉了。

import (
	"encoding/json"
	"testing"
)

// TestUsageFromJSONParsesCachedChatCompletions chat/completions 的缓存字段。
func TestUsageFromJSONParsesCachedChatCompletions(t *testing.T) {
	var root map[string]any
	body := `{"usage":{"prompt_tokens":1000,"completion_tokens":50,"total_tokens":1050,
		"prompt_tokens_details":{"cached_tokens":960}}}`
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		t.Fatal(err)
	}
	got := usageFromJSON(root["usage"])
	if got.InputTokens != 1000 || got.CachedTokens != 960 {
		t.Fatalf("解析结果不对：%+v", got)
	}
	if !got.CachedReported {
		t.Error("上游报了这个字段，CachedReported 应为 true")
	}
}

// TestUsageFromJSONParsesCachedResponses responses 的字段名不同，同样要认。
func TestUsageFromJSONParsesCachedResponses(t *testing.T) {
	var root map[string]any
	body := `{"usage":{"input_tokens":800,"output_tokens":40,
		"input_tokens_details":{"cached_tokens":800}}}`
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		t.Fatal(err)
	}
	got := usageFromJSON(root["usage"])
	if got.InputTokens != 800 || got.CachedTokens != 800 || !got.CachedReported {
		t.Fatalf("responses 协议的缓存字段没认出来：%+v", got)
	}
}

// TestUsageFromJSONDistinguishesZeroFromMissing 「报了 0」与「没报」必须分开。
//
// 这是本批最容易写错、也最难发现的一处：两者的 CachedTokens 都是 0，
// 只有 CachedReported 能区分。写错时页面上的命中率会偏低，而这个症状会被
// 归因到提示词上（去改规则、改顺序），没人会想到是上游没报。
func TestUsageFromJSONDistinguishesZeroFromMissing(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantZero bool // 是否「报了 0」
	}{
		{"报 0", `{"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0}}}`, true},
		{"完全没有 details", `{"usage":{"prompt_tokens":100,"completion_tokens":5}}`, false},
		{"details 是空对象", `{"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{}}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root map[string]any
			if err := json.Unmarshal([]byte(tc.body), &root); err != nil {
				t.Fatal(err)
			}
			got := usageFromJSON(root["usage"])
			if got.CachedTokens != 0 {
				t.Fatalf("这两种情况的值都该是 0，实得 %d", got.CachedTokens)
			}
			if got.CachedReported != tc.wantZero {
				t.Errorf("CachedReported 应为 %v，实得 %v —— 判据是「字段在不在」，不是「值大不大」",
					tc.wantZero, got.CachedReported)
			}
		})
	}
}

// TestPercentageGuardsZeroDenominator 分母为 0 时回 0 而不是 NaN。
//
// NaN 会渲染成「NaN%」且不报错、不进日志 —— 页面上出现一个谁也不认识的字符，
// 而排查时从这条线索出发什么都查不到。
func TestPercentageGuardsZeroDenominator(t *testing.T) {
	if got := percentage(10, 0); got != 0 {
		t.Errorf("分母为 0 应回 0，实得 %v", got)
	}
	if got := percentage(0, 0); got != 0 {
		t.Errorf("0/0 应回 0，实得 %v", got)
	}
	if got := percentage(960, 1000); got < 95.9 || got > 96.1 {
		t.Errorf("960/1000 应约 96.0，实得 %v", got)
	}
}

// TestPercentTextSeparatesNoData 没数据与 0% 的展示必须不同。
func TestPercentTextSeparatesNoData(t *testing.T) {
	if got := percentText(0, false); got != "—" {
		t.Errorf("没数据时应显示 —，实得 %q", got)
	}
	if got := percentText(0, true); got != "0.0%" {
		t.Errorf("确实报了 0 时应显示 0.0%%，实得 %q", got)
	}
	if got := percentText(96.234, true); got != "96.2%" {
		t.Errorf("应保留一位小数，实得 %q", got)
	}
}
