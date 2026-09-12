package feature

// automation_trigger_test.go — 事件触发与延时调度（issue #38 P3）。
//
// 触发是引擎的入口侧：业务动作发生 → 找到匹配的启用流程 → 启动实例。
// 调度是兜底侧：主路径（队列延时）失效时把到点的实例重新投递。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// TestOpenEventStartsAutomation 打开事件触发流程，重复触发不会重复启动。
func TestOpenEventStartsAutomation(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "opener@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "打开后跟进", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "tag", map[string]any{"add": []any{"opened_mail"}}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerEmailOpened)

	// 没发生过打开事件时，触发不该启动（流程匹配的是「打开了」这件事，
	// 而不是「调用了一次 OnEmailOpened」—— 事件本身由追踪 worker 落库）。
	f.svc.OnEmailOpened(ctx, contact.ID)
	_, total, err := f.m.ListRuns(ctx, id, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("触发应启动 1 个实例，实际 %d", total)
	}
	// 再次触发（同一个人又打开一次）：幂等，不该再建实例。
	f.svc.OnEmailOpened(ctx, contact.ID)
	_, total, _ = f.m.ListRuns(ctx, id, "", 0, 10)
	if total != 1 {
		t.Fatalf("重复触发不该新建实例，实际 %d", total)
	}
}

// TestSubscribedTriggerFiresOnTransition 订阅触发只在「非订阅 → 订阅」时发生。
func TestSubscribedTriggerFiresOnTransition(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "sub@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusPending,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "订阅欢迎", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "tag", map[string]any{"add": []any{"welcomed"}}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerContactSubscribed)

	if err := f.svc.UpdateContactStatus(ctx, &maildto.UpdateContactStatusReq{
		ID: contact.ID, Status: mailmodel.ContactStatusSubscribed,
	}); err != nil {
		t.Fatal(err)
	}
	_, total, _ := f.m.ListRuns(ctx, id, "", 0, 10)
	if total != 1 {
		t.Fatalf("订阅变更应触发，实际 %d 个实例", total)
	}
	// 再保存一次「订阅」（状态没变）：不该再触发 —— 否则重复点保存就多塞一条欢迎流程。
	if err := f.svc.UpdateContactStatus(ctx, &maildto.UpdateContactStatusReq{
		ID: contact.ID, Status: mailmodel.ContactStatusSubscribed,
	}); err != nil {
		t.Fatal(err)
	}
	_, total, _ = f.m.ListRuns(ctx, id, "", 0, 10)
	if total != 1 {
		t.Fatalf("状态未变化时不该重复触发，实际 %d 个实例", total)
	}
}

// TestSchedulerPicksUpDueRuns 调度兜底：把到点的等待实例重新投递。
func TestSchedulerPicksUpDueRuns(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "due@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "延时兜底", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "delay", map[string]any{"minutes": 1}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	// 还没到点：扫不到。
	queued, err := f.svc.EnqueueDueRuns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("未到点不该被扫到，实际 %d", queued)
	}
	// 把 next_run_at 拨到过去（模拟时间已到），再扫一轮。
	past := time.Now().Add(-time.Minute)
	if err := f.m.UpdateRunFields(ctx, run.ID, map[string]any{"next_run_at": past}); err != nil {
		t.Fatal(err)
	}
	queued, err = f.svc.EnqueueDueRuns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("到点的实例应被扫到，实际 %d", queued)
	}
}

// TestInactiveAutomationDoesNotStart 未启用的流程不接受新实例。
func TestInactiveAutomationDoesNotStart(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "draft@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	// 建流程但**不启用**（保持 draft）。
	def := map[string]any{"entry": "n1", "nodes": []any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "end", nil, ""),
	}}
	raw, _ := json.Marshal(def)
	item, err := f.svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name: "草稿流程", TriggerType: mailmodel.TriggerManual, Definition: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := f.svc.StartRun(ctx, item.ID, contact.ID, mailmodel.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if started {
		t.Fatal("草稿流程不该接受新实例")
	}
	count, _ := f.svc.EnqueueDueRuns(ctx, 10)
	if count != 0 {
		t.Fatalf("不该有到点实例，实际 %d", count)
	}
}
