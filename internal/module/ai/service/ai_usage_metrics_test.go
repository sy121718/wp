package aiservice

import (
	json "go_wp/pkg/jsonx"
	"testing"
)

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

func TestUsageFromJSONDistinguishesZeroFromMissing(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantZero bool
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
