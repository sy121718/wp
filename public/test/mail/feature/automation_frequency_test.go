package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

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

func mustJSONTree(t *testing.T, nodes []map[string]any) ([]byte, error) {
	t.Helper()
	list := make([]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	return json.Marshal(map[string]any{"schema": 2, "entry": "n1", "nodes": list})
}

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

func TestExitOnReplyStopsOnlyConfiguredFlow(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

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

	for _, id := range []uint64{exitFlow, keepFlow} {
		if ok, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerEmailOpened); err != nil || !ok {
			t.Fatalf("建实例失败（flow=%d）: ok=%v err=%v", id, ok, err)
		}
	}

	f.svc.OnEmailReplied(ctx, contact.ID)

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

	keepRun, err := m.GetRun(ctx, findRunID(t, m, keepFlow, contact.ID))
	if err != nil {
		t.Fatal(err)
	}
	if keepRun.Status != mailmodel.RunStatusRunning {
		t.Fatalf("没配 exit_on_reply 的流程不该被停，实际 %q", keepRun.Status)
	}
}

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
