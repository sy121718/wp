package aiservice

import (
	"testing"

	json "go_wp/pkg/jsonx"

	aidto "go_wp/internal/module/ai/dto"
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

// 百分比文本已归模板（`percent` 函数 + 模板里的 — 分支），service 不再拼成品串。
// 这里只锁「有没有数据」这条业务事实 —— 它决定了模板走哪个分支。
func TestHitRateReadySeparatesNoData(t *testing.T) {
	// ready=false 是「供应商没上报缓存字段」，不是「命中率 0」。
	// 这两件事在页面上必须可区分：前者显示 —，后者显示 0.0%。
	noData := aidto.SessionUsage{HitRateReady: false, HitRatePct: 0}
	reportedZero := aidto.SessionUsage{HitRateReady: true, HitRatePct: 0}
	if noData.HitRateReady {
		t.Error("没数据时 HitRateReady 应为 false")
	}
	if !reportedZero.HitRateReady {
		t.Error("确实上报了 0 时 HitRateReady 应为 true")
	}
}
