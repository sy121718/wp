package mailservice

// mail_automation_start_test.go — 批量触发路径的两个**纯逻辑**判据（不碰数据库）。
//
// 为什么单独测：这两个函数错了都会静默变成「流程不触发」而不是报错 ——
//   · automationsForTrigger 的匹配条件漏了 / 多了，实例数就悄悄不对；
//   · hasTriggerType 若被替换成「带参数匹配的过滤」（很自然的顺手写法），
//     extra 为 nil 时**带 tag 条件的流程不会被匹配上**，粗判假阴性 →
//     导入路径会以为「没有 tag_added 流程」而跳过整批标签触发。

import (
	"testing"

	mailmodel "go_wp/internal/module/mail/model"
)

func TestAutomationsForTriggerMatching(t *testing.T) {
	list := []*mailmodel.MailAutomationEntity{
		{ID: 1, TriggerType: mailmodel.TriggerTagAdded, TriggerParams: mailmodel.JSONMap{"tag": "vip"}},
		{ID: 2, TriggerType: mailmodel.TriggerTagAdded},
		{ID: 3, TriggerType: mailmodel.TriggerContactCreated},
		nil, // 防御：nil 元素不该让过滤 panic
	}

	// extra 为 nil：无 tag 条件的流程算匹配，带 tag 条件的不匹配
	//（gotTag 为空 → wantTag 匹配不上）——这正是「不能用它做粗判」的原因。
	got := automationsForTrigger(list, mailmodel.TriggerTagAdded, nil)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("extra=nil 时只应匹配无条件的 tag_added 流程（实际 %d 条）", len(got))
	}

	// 带 extra：按标签匹配，无条件的流程仍通配。
	got = automationsForTrigger(list, mailmodel.TriggerTagAdded, map[string]any{"tag": "vip"})
	if len(got) != 2 {
		t.Fatalf("tag=vip 应匹配 2 条（带条件的那条 + 无条件通配），实际 %d", len(got))
	}

	got = automationsForTrigger(list, mailmodel.TriggerContactCreated, nil)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("contact_created 应匹配 1 条，实际 %d", len(got))
	}

	if got := automationsForTrigger(nil, mailmodel.TriggerTagAdded, nil); len(got) != 0 {
		t.Fatalf("空流程集不该匹配出任何流程，实际 %d", len(got))
	}
}

func TestHasTriggerTypeIgnoresParams(t *testing.T) {
	list := []*mailmodel.MailAutomationEntity{
		// 只带 tag 条件的流程：粗判必须认定「存在 tag_added 流程」。
		{ID: 1, TriggerType: mailmodel.TriggerTagAdded, TriggerParams: mailmodel.JSONMap{"tag": "vip"}},
	}
	if !hasTriggerType(list, mailmodel.TriggerTagAdded) {
		t.Fatal("带 tag 条件的流程也算「有 tag_added 流程」—— 粗判不能走参数匹配")
	}
	if hasTriggerType(list, mailmodel.TriggerContactCreated) {
		t.Fatal("没有该类流程时不该判为存在")
	}
	if hasTriggerType(nil, mailmodel.TriggerTagAdded) {
		t.Fatal("空流程集不该判为存在")
	}
}
