package orderhttp

// order_bulk_summary_test.go — 批量动作结论文案的四个分支。
//
// 为什么值得单独测：bulkSummary 的结果直接进 ?done= / ?err= 显示给用户，
// 分支写错不会报错、不会失败，只会让人看到与实际不符的结论
//（「已发货 5 个」实际只成 2 个 / 把「全部被跳过」说成成功）。
// 这与模块内就近单测的定位一致：纯函数、不碰数据库。
//
// 动词 / 名词现在是词条（key + 中文原文），bulkSummary 从 c 取当前语言 ——
// 断言里的动词与名词文本一律从**同一个取法**（orderBulkTextOf）取，不手抄中文：
// 手抄的第二份真相会在改词条时静默漂移。

import (
	"strings"
	"testing"
)

func TestBulkSummary_Branches(t *testing.T) {
	c := orderBulkCtx(t)
	verb := orderBulkVerbFlowed
	noun := orderBulkNounOrder
	verbText := orderBulkTextOf(c, verb)
	nounText := orderBulkTextOf(c, noun)
	cases := []struct {
		name        string
		done        int
		skipped     int
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "一个都没勾选：不能报成成功",
			done:        0,
			skipped:     0,
			wantContain: []string{"没有勾选"},
			wantAbsent:  []string{verbText, "跳过"},
		},
		{
			name:        "全成功：只报成功数，不提跳过",
			done:        3,
			skipped:     0,
			wantContain: []string{verbText, "3", nounText},
			wantAbsent:  []string{"跳过"},
		},
		{
			name:        "全部被跳过：必须说清 0 个成功，不能像成功",
			done:        0,
			skipped:     2,
			wantContain: []string{"0", nounText, "2", "跳过"},
		},
		{
			name:        "部分成功：成功数与跳过数都要出现",
			done:        3,
			skipped:     2,
			wantContain: []string{verbText, "3", nounText, "跳过", "2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bulkSummary(c, verb, noun, tc.done, tc.skipped)
			if strings.TrimSpace(got) == "" {
				t.Fatal("结论文案不该为空")
			}
			for _, w := range tc.wantContain {
				if !strings.Contains(got, w) {
					t.Errorf("文案缺少 %q：%s", w, got)
				}
			}
			for _, w := range tc.wantAbsent {
				if strings.Contains(got, w) {
					t.Errorf("文案不该出现 %q：%s", w, got)
				}
			}
		})
	}
}

// 未勾选与「全部被跳过」是两件事，文案必须能区分（都出现「没有/0」容易混）。
func TestBulkSummary_NothingSelectedDiffersFromAllSkipped(t *testing.T) {
	c := orderBulkCtx(t)
	none := bulkSummary(c, orderBulkVerbFlowed, orderBulkNounOrder, 0, 0)
	allSkipped := bulkSummary(c, orderBulkVerbFlowed, orderBulkNounOrder, 0, 2)
	if none == allSkipped {
		t.Fatalf("未勾选与全部被跳过的文案相同：%q", none)
	}
}

// TestBulkSummaryTemplatesAreKeyedAndTranslated 四条模板、七个动词、三个名词都必须是词条。
//
// 这条钉住的是「key 化本身没被改写回去」：只要有人在 bulkSummary 里重新写中文字面量，
// 或者忘了把新动词登记进 orderBulkActions，这里或 TestOrderBulkActionsCoverEveryCallSite 就会红。
func TestBulkSummaryTemplatesAreKeyedAndTranslated(t *testing.T) {
	all := []orderBulkText{
		orderBulkNothingSelected, orderBulkAllDone, orderBulkAllSkipped, orderBulkPartial,
		orderBulkVerbFlowed, orderBulkVerbCancelled, orderBulkVerbApproved, orderBulkVerbRejected,
		orderBulkVerbDeleted, orderBulkVerbDisabled, orderBulkVerbEnabled,
		orderBulkNounOrder, orderBulkNounReturn, orderBulkNounCoupon,
		couponBulkTargetInvalidText,
	}
	for _, tpl := range all {
		if !strings.HasPrefix(tpl.key, "order.bulk.") {
			t.Errorf("批量结论文案的 key 必须以 order.bulk. 开头，实际 %q", tpl.key)
		}
		if strings.TrimSpace(tpl.fallback) == "" {
			t.Errorf("%s 缺少中文原文（词条缺失时的兜底）", tpl.key)
		}
	}
}
