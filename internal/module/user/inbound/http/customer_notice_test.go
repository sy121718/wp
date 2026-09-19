package userhttp

// customer_notice_test.go — 客户列表页 ?done=（批量摘要）读侧受控出口的回归。
//
// 为什么必须有：这条通道此前**只靠 Jet 的 HTML 转义** —— 转义只挡「脚本执行」，
// 不挡「伪造系统提示」：手拼 ?done=<任意文案> 会以系统口吻显示在页面上，
// 与直出内部错误同级（都是「响应不是可信边界」）。
//
// 而它的判定又比同批其它页面难：别的 ?done= 是固定 token 或单句文案，
// 这里是**带计数的动态整句**（「批量操作：已停用 3 个，1 个未处理（…）。」），
// 最容易在收口时被顺手放过。所以两侧都要钉：
//   · 写侧真实产出的每一种句子都必须被放行（否则成功回执**静默消失**）；
//   · 手拼的一律落空串。

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"
)

func customerDoneCtx(raw string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/user/customers?done="+url.QueryEscape(raw), nil)
	return c
}

// TestCustomerPageDoneAcceptsEveryWriterShape 写侧每一种分支都必须被读侧认出。
func TestCustomerPageDoneAcceptsEveryWriterShape(t *testing.T) {
	counts := []int{0, 1, 3, 128}
	var shapes []string
	// 空结果两支。
	shapes = append(shapes, customerBulkSummary("", 0, 0), customerBulkUnlockSummary(0, 0, 0))
	for _, status := range []int{customerStatusActive, customerStatusDisabled} {
		verb := customerStatusActionVerb(status)
		for _, done := range counts {
			for _, skipped := range counts {
				shapes = append(shapes, customerBulkSummary(verb, done, skipped))
			}
		}
	}
	for _, unlocked := range counts {
		for _, noop := range counts {
			for _, skipped := range counts {
				shapes = append(shapes, customerBulkUnlockSummary(unlocked, noop, skipped))
			}
		}
	}
	for _, s := range shapes {
		if got := customerPageDone(customerDoneCtx(s)); got != strings.TrimSpace(s) {
			t.Fatalf("写侧产出的摘要必须被读侧放行，实际被拒：%q（读侧得到 %q）", s, got)
		}
	}
}

// TestCustomerPageDoneRejectsForged 手拼的必须一律落空串。
func TestCustomerPageDoneRejectsForged(t *testing.T) {
	forged := []string{
		"",
		"   ",
		"系统内部错误，请稍后重试",
		"已删除 3 个商品。",   // 别的模块的文案
		"批量操作：已启用 3 个", // 少了句号：必须整体相等
		// 注意：「批量操作：已启用 3 个。」**不是**伪造 —— 那是写侧 done=3 / skipped=0 的正常输出，
		// 已归在上一条用例的「必须放行」里。把合法输出写进伪造清单会让这条测试自相矛盾。
		"批量操作：已启用 3 个，1 个未处理（账号不存在）。",                  // 括号里的理由串错了（那是解锁动作的理由）
		"批量解除锁定：已解除锁定 2 个，2 个未处理（账号不存在，或当前状态不允许这个动作）。", // 理由串串了通道
		"批量操作：已启用 3 个，9 个未处理（账号不存在，或当前状态不允许这个动作）。脚本",   // 后缀夹带
		"<script>alert(1)</script>批量操作：已启用 1 个。",       // 前缀夹带
		strings.Repeat("批量操作：已启用 1 个。", 80),            // 超长（>512 字节）
	}
	for _, raw := range forged {
		if got := customerPageDone(customerDoneCtx(raw)); got != "" {
			t.Fatalf("伪造回执不该命中，实际放行 %q（输入 %q）", got, raw)
		}
	}
}

// TestCustomerPageDoneCandidatesAreDerivedNotHandWritten 候选集合必须由写侧派生。
//
// 手抄一份候选的下场是「写侧改了措辞、读侧再也认不出」，而那表现为成功回执**静默消失**
// （不报错、日志里也没有）。这里断言候选里确实含有写侧每种动词的输出 ——
// 有人把候选改成硬编码清单并漏了一个动词时，这条会红。
func TestCustomerPageDoneCandidatesAreDerivedNotHandWritten(t *testing.T) {
	for _, status := range []int{customerStatusActive, customerStatusDisabled} {
		verb := customerStatusActionVerb(status)
		want := shell.NoticeTemplate(customerBulkSummary(verb, 2, 0))
		found := false
		for _, cand := range customerBulkNoticeCandidates {
			if cand == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("候选集合缺少动词 %q 的写侧输出（%q）—— 候选必须由写侧函数派生", verb, want)
		}
	}
}
