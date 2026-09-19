package orderhttp

// order_notice_test.go — 三个列表页（订单 / 退货申请 / 优惠码）?done= 的受控出口回归。
//
// 为什么必须有：批量动作的结论带计数（「已发货 3 个订单，跳过 2 个（状态不允许或已不存在）。」），
// 三个页面此前都是 `"Done": strings.TrimSpace(c.Query("done"))` 原样进模板 —— 手拼一个
// /admin/orders?done=任意文案 就能往页面上塞一条顶着「成功」样式的伪造消息。
//
// 收口之后最容易静默出错的不是「伪造能进来」，而是**写侧换一个词动词就再也认不出来**：
// 批量结论的措辞与动词组合都在写侧调用点上（已流转 / 已取消 / 已同意 / 已拒绝 / 已删除 /
// 已停用 / 已启用 × 订单 / 退货申请 / 优惠码）。所以这里按 orderBulkActions × 四个分支
// 穷举一遍：写侧真会产出多少种文案，读侧就要认多少种。

import (
	"strings"
	"testing"
)

// TestOrderPageDoneAcceptsEveryWriterShape 写侧能产出的每一种结局都要被读侧认出来。
func TestOrderPageDoneAcceptsEveryWriterShape(t *testing.T) {
	if len(orderBulkActions) == 0 {
		t.Fatal("orderBulkActions 为空 —— 读侧候选没有来源，这条测试就失去意义了")
	}
	for _, action := range orderBulkActions {
		verb, noun := action[0], action[1]
		for _, done := range []int{0, 1, 3} {
			for _, skipped := range []int{0, 1, 3} {
				msg := bulkSummary(verb, noun, done, skipped)
				if msg == "" {
					t.Fatalf("写侧文案为空：%s / %s / done=%d skipped=%d", verb, noun, done, skipped)
				}
				if got := orderPageDone(msg); got != msg {
					t.Errorf("写侧结论读侧认不出来（会变成「没有这条提示」）：got %q want %q", got, msg)
				}
			}
		}
	}
	// 优惠码页还有一条不走 bulkSummary、但同样进 ?done= 的参数级提示。
	if got := orderPageDone(couponBulkTargetInvalidText); got != couponBulkTargetInvalidText {
		t.Errorf("批量启停的目标状态非法提示应被放行，实际 %q", got)
	}
}

// TestOrderPageDoneRejectsForged 不是写侧产出的取值一律落空串。
//
// 落空串而不是归口文案：成功提示没有「必须说点什么」的语义，
// 在成功的位置上顶一条错误提示比什么都不显示更糟。
func TestOrderPageDoneRejectsForged(t *testing.T) {
	msg := bulkSummary("已流转", "订单", 3, 0)
	for _, raw := range []string{
		"",
		"   ",
		"订单已全部发货，感谢使用",
		"已删除 3 个块。",                       // 别的模块的受控文案，本页不该认
		"没有勾选任何优惠券。",                      // 名词差一个字（优惠码 ≠ 优惠券）
		"<script>alert(1)</script>" + msg, // 夹带
		msg + "<script>alert(1)</script>",
		strings.Repeat(msg, 100),
	} {
		if got := orderPageDone(raw); got != "" {
			t.Errorf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
		}
	}
}

// TestOrderBulkActionsCoverEveryCallSite 动词 / 名词表必须覆盖各页 bulkSummary 的全部调用组合。
//
// 这张表是**读侧候选的唯一来源**：写侧新增一个批量动作却忘了登记，症状是该页
// 「批量操作完成了却没有回执」。这条断言把当前的全部组合写死在测试里，
// 新增调用点时至少会被提醒一次（要么补表、要么确认它走别的通道）。
func TestOrderBulkActionsCoverEveryCallSite(t *testing.T) {
	want := map[string]bool{
		"已流转|订单":   true,
		"已取消|订单":   true,
		"已同意|退货申请": true,
		"已拒绝|退货申请": true,
		"已删除|优惠码":  true,
		"已停用|优惠码":  true,
		"已启用|优惠码":  true,
	}
	if len(orderBulkActions) != len(want) {
		t.Fatalf("动作表条目数变了（%d，期望 %d）—— 新增 / 删除批量动作时请同步更新本用例",
			len(orderBulkActions), len(want))
	}
	for _, action := range orderBulkActions {
		key := action[0] + "|" + action[1]
		if !want[key] {
			t.Errorf("出现了未登记在用例里的动作组合 %q", key)
		}
	}
}
