package feature

// automation_frequency_test.go — 触发频率与「回信即退出」（T7）。
//
// 两条判据都是「不报错但会出事」：
//
//  1. `once` 挡不住的话，同一个人会在每次打开邮件时重跑整条欢迎流程 —— 而
//     `idx_mail_automation_runs_active` 只挡 running/waiting，跑完的实例不在里面，
//     所以这条闸门必须由应用层「有过任何实例」来判断；
//  2. `exit_on_reply` 停错对象的后果更直接：同一个联系人可能同时在多条流程里，
//     停掉没配这个开关的那条就是「配了 A，B 跟着停了」。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// seedFrequencyAutomation 建一条带频率配置的启用流程（触发 → 打标签 → 结束）。
func seedFrequencyAutomation(t *testing.T, f *mailFixture, name, frequency string, exitOnReply bool) uint64 {
	t.Helper()
	ctx := context.Background()
	raw, err := mustJSONTree(t, []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "tag", map[string]any{"add": []any{"ran_" + name}}, "n3"),
		newNode("n3", "end", nil, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name: name, TriggerType: mailmodel.TriggerEmailOpened, Definition: raw,
		TriggerFrequency: frequency, ExitOnReply: &exitOnReply,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	if err := f.svc.SetAutomationStatus(ctx, &maildto.SetAutomationStatusReq{
		ID: item.ID, Status: mailmodel.AutomationStatusActive,
	}); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	return item.ID
}

// mustJSONTree 把一组节点包成 v2 定义。
func mustJSONTree(t *testing.T, nodes []map[string]any) ([]byte, error) {
	t.Helper()
	list := make([]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	return json.Marshal(map[string]any{"schema": 2, "entry": "n1", "nodes": list})
}

// TestOnceFrequencyOnlyCreatesOneRun once：同一个人第二次触发不再建实例。
func TestOnceFrequencyOnlyCreatesOneRun(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedFrequencyAutomation(t, f, "once_flow", mailmodel.TriggerFrequencyOnce, false)
	if err := f.seedContact(ctx, "once@example.com", "once", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	m := mailmodel.NewMailModel(f.db)
	contact, err := m.GetContactByEmail(ctx, "once@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// 第一次触发 → 建实例并跑完。
	if ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened); err != nil || !ok {
		t.Fatalf("首次触发应建实例: ok=%v err=%v", ok, err)
	}
	run, err := m.ActiveRun(ctx, id, contact.ID)
	if err != nil {
		t.Fatalf("取实例失败: %v", err)
	}
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进失败: %v", err)
	}

	// 第二次触发 → **不**建实例（判据是「有过任何实例」，跑完的也算）。
	ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened)
	if err != nil {
		t.Fatalf("第二次触发不该报错: %v", err)
	}
	if ok {
		t.Fatal("frequency=once 时第二次触发不该建实例（跑完的实例就是不该再跑的理由）")
	}
	_, total, err := m.ListRuns(ctx, id, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("once 流程总共应只有 1 个实例，实际 %d", total)
	}
}

// TestEveryTimeFrequencyCreatesTwoRuns every_time：两次触发各建一个实例。
func TestEveryTimeFrequencyCreatesTwoRuns(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedFrequencyAutomation(t, f, "every_flow", mailmodel.TriggerFrequencyEveryTime, false)
	if err := f.seedContact(ctx, "every@example.com", "every", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	m := mailmodel.NewMailModel(f.db)
	contact, err := m.GetContactByEmail(ctx, "every@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened); err != nil || !ok {
		t.Fatalf("首次触发应建实例: ok=%v err=%v", ok, err)
	}
	run, err := m.ActiveRun(ctx, id, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进失败: %v", err)
	}

	// 第二个实例必须等第一个结束才能 running —— 那由部分唯一索引兜住
	// （idx_mail_automation_runs_active）。这里第一个已经 completed，所以能建。
	if ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened); err != nil || !ok {
		t.Fatalf("every_time 应能建第二个实例: ok=%v err=%v", ok, err)
	}
	_, total, err := m.ListRuns(ctx, id, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("every_time 流程应有 2 个实例，实际 %d", total)
	}
}

// TestExitOnReplyStopsOnlyConfiguredFlow 只停配了 exit_on_reply 的那条。
func TestExitOnReplyStopsOnlyConfiguredFlow(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 两条启用流程，只有第二条配了 exit_on_reply。
	exitFlow := seedFrequencyAutomation(t, f, "exit_flow", mailmodel.TriggerFrequencyEveryTime, true)
	keepFlow := seedFrequencyAutomation(t, f, "keep_flow", mailmodel.TriggerFrequencyEveryTime, false)

	if err := f.seedContact(ctx, "reply@example.com", "reply", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	m := mailmodel.NewMailModel(f.db)
	contact, err := m.GetContactByEmail(ctx, "reply@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// 两条流程各建一个实例（都停在 running，不推进）。
	for _, id := range []uint64{exitFlow, keepFlow} {
		if ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened); err != nil || !ok {
			t.Fatalf("建实例失败（flow=%d）: ok=%v err=%v", id, ok, err)
		}
	}

	f.svc.OnEmailReplied(ctx, contact.ID)

	// 配了 exit_on_reply 的：stopped。
	exitRun, err := m.GetRun(ctx, findRunID(t, m, exitFlow, contact.ID))
	if err != nil {
		t.Fatal(err)
	}
	if exitRun.Status != mailmodel.RunStatusStopped {
		t.Fatalf("exit_on_reply=true 的流程实例应被停掉，实际 %q", exitRun.Status)
	}
	if exitRun.ErrorMessage == nil || *exitRun.ErrorMessage == "" {
		t.Fatal("停掉时该写下原因（排障要知道为什么退出）")
	}

	// 没配的：仍是 running（停错那条就是「配了 A，B 也跟着停了」）。
	keepRun, err := m.GetRun(ctx, findRunID(t, m, keepFlow, contact.ID))
	if err != nil {
		t.Fatal(err)
	}
	if keepRun.Status != mailmodel.RunStatusRunning {
		t.Fatalf("没配 exit_on_reply 的流程不该被停，实际 %q", keepRun.Status)
	}
}

// TestExitOnReplyDoesNothingWhenDisabled 没配 exit_on_reply 时回信不影响实例。
func TestExitOnReplyDoesNothingWhenDisabled(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedFrequencyAutomation(t, f, "no_exit_flow", mailmodel.TriggerFrequencyEveryTime, false)
	if err := f.seedContact(ctx, "noexit@example.com", "noexit", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	m := mailmodel.NewMailModel(f.db)
	contact, err := m.GetContactByEmail(ctx, "noexit@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened); err != nil || !ok {
		t.Fatalf("建实例失败: ok=%v err=%v", ok, err)
	}
	f.svc.OnEmailReplied(ctx, contact.ID)

	run, err := m.GetRun(ctx, findRunID(t, m, id, contact.ID))
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != mailmodel.RunStatusRunning {
		t.Fatalf("exit_on_reply=false 时回信不该改实例状态，实际 %q", run.Status)
	}
}

// findRunID 取某联系人某流程的实例 id（先找进行中的，再找最新的）。
func findRunID(t *testing.T, m *mailmodel.MailModel, automationID, contactID uint64) uint64 {
	t.Helper()
	ctx := context.Background()
	if run, err := m.ActiveRun(ctx, automationID, contactID); err == nil {
		return run.ID
	}
	runs, _, err := m.ListRuns(ctx, automationID, "", 0, 10)
	if err != nil || len(runs) == 0 {
		t.Fatalf("找不到实例（automation=%d contact=%d）: %v", automationID, contactID, err)
	}
	for _, r := range runs {
		if r.ContactID == contactID {
			return r.ID
		}
	}
	t.Fatalf("找不到联系人 %d 的实例", contactID)
	return 0
}
