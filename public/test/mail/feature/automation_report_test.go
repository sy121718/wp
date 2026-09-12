package feature

// automation_report_test.go — 运行实例的排障视图（issue #38 P3，目标 ⑥）。
//
// 排障视图的价值全在「一句话解释」上：光有 status 字段（running / waiting / failed）
// 运营看不懂，也无法判断该不该管。所以测试重点钉 Explain。

import (
	"context"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// TestRunDetailExplainsWaiting 等待态解释出「在等」与「何时继续」。
func TestRunDetailExplainsWaiting(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	name := "张三"
	contact := &mailmodel.MailContactEntity{
		Email: "explain@example.com", Name: &name,
		Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "解释流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "delay", map[string]any{"minutes": 120}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	detail, err := f.svc.AutomationRunDetail(ctx, run.ID)
	if err != nil {
		t.Fatalf("取排障详情失败: %v", err)
	}
	if detail.Email != "explain@example.com" {
		t.Fatalf("详情应带联系人邮箱（只给 id 对排障没有帮助），实际 %q", detail.Email)
	}
	if detail.AutomationName != "解释流程" {
		t.Fatalf("详情应带流程名，实际 %q", detail.AutomationName)
	}
	if !strings.Contains(detail.Explain, "等待中") || !strings.Contains(detail.Explain, "继续") {
		t.Fatalf("等待态解释应说明在等、何时继续，实际 %q", detail.Explain)
	}
	if detail.TotalNodes != 3 {
		t.Fatalf("进度分母应为流程节点数 3，实际 %d", detail.TotalNodes)
	}
	if detail.DoneNodes != 2 {
		t.Fatalf("已执行节点应为 2（trigger + delay），实际 %d", detail.DoneNodes)
	}
	if len(detail.Timeline) != 2 {
		t.Fatalf("时间线应有 2 条，实际 %d", len(detail.Timeline))
	}
	// 时间线按执行顺序（trigger 在前）。
	if detail.Timeline[0].NodeType != "trigger" || detail.Timeline[1].NodeType != "delay" {
		t.Fatalf("时间线顺序不对: %+v", detail.Timeline)
	}

	// 列表也应带邮箱（排障第一眼要看到「是谁」）。
	list, err := f.svc.ListAutomationRuns(ctx, &maildto.AutomationRunListReq{AutomationID: id, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Email != "explain@example.com" {
		t.Fatalf("列表应带邮箱，实际 %+v", list.Items)
	}
	if list.Counts[mailmodel.RunStatusWaiting] != 1 {
		t.Fatalf("状态统计应有 1 个等待中，实际 %+v", list.Counts)
	}
}

// TestRunDetailExplainsFailure 失败态解释出「失败在哪一步、为什么」。
func TestRunDetailExplainsFailure(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "fail@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	// 条件写成不认识的值：图校验放行（字符串形状没问题），执行期会失败。
	id := seedAutomation(t, f, "会失败的流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		map[string]any{
			"key": "n2", "type": "branch",
			"params": map[string]any{"conditions": []any{"外星人登录过"}},
			"yes":    "n3", "no": "n3",
		},
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进本身不该返回错误（失败被记进实例状态）: %v", err)
	}
	row, _ := f.m.GetRun(ctx, run.ID)
	if row.Status != mailmodel.RunStatusFailed {
		t.Fatalf("未知条件应让实例失败，实际 %s", row.Status)
	}
	detail, err := f.svc.AutomationRunDetail(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Explain, "失败") || !strings.Contains(detail.Explain, "外星人登录过") {
		t.Fatalf("失败解释应说明哪一步、什么原因，实际 %q", detail.Explain)
	}
}

// TestRunDetailExplainsCompleted 完成态解释出已完成。
func TestRunDetailExplainsCompleted(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "done@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "会完成的流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	detail, err := f.svc.AutomationRunDetail(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Explain, "已完成") {
		t.Fatalf("完成态解释应说明已完成，实际 %q", detail.Explain)
	}
}

// TestRunDetailNotFound 不存在的实例给出明确错误。
func TestRunDetailNotFound(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	if _, err := f.svc.AutomationRunDetail(context.Background(), 999999); err == nil {
		t.Fatal("不存在的实例应报错")
	}
	if _, err := f.svc.AutomationRunDetail(context.Background(), 0); err == nil {
		t.Fatal("run_id 为 0 应报错")
	}
}
