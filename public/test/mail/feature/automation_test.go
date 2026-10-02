package feature

// automation_test.go — 自动化执行器（issue #38 P3）。
//
// 覆盖执行模型的三条核心语义：
//  1. 一路走到结束节点，每步留日志；
//  2. 等待节点**推进游标后挂起**，唤醒时从下一步继续（不会重等一次）；
//  3. **幂等**：重复推进不会重复执行动作（同一个人不会收到两封一样的邮件）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// seedAutomation 建一个启用中的流程，返回 id。
func seedAutomation(t *testing.T, f *fixture, name string, entry string, nodes []map[string]any, trigger string) uint64 {
	t.Helper()
	list := make([]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	def := map[string]any{"entry": entry, "nodes": list}
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.svc.SaveAutomation(context.Background(), &maildto.SaveAutomationReq{
		Name: name, TriggerType: trigger, Definition: raw,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	if err := f.svc.SetAutomationStatus(context.Background(), &maildto.SetAutomationStatusReq{
		ID: item.ID, Status: mailmodel.AutomationStatusActive,
	}); err != nil {
		t.Fatalf("启用流程失败: %v", err)
	}
	return item.ID
}

func newNode(key, typ string, params map[string]any, next string) map[string]any {
	n := map[string]any{"key": key, "type": typ}
	if params != nil {
		n["params"] = params
	}
	if next != "" {
		n["next"] = next
	}
	return n
}

// TestAutomationRunsToEndAndLogsEachStep 一路走到结束，每步都有日志。
func TestAutomationRunsToEndAndLogsEachStep(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "auto@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "欢迎流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "tag", map[string]any{"add": []any{"greeted"}}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)

	started, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual)
	if err != nil || !started {
		t.Fatalf("启动失败 started=%v err=%v", started, err)
	}
	run, err := f.m.ActiveRun(ctx, id, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进失败: %v", err)
	}
	row, err := f.m.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != mailmodel.RunStatusCompleted {
		t.Fatalf("应完成，实际 %s（错误 %v）", row.Status, row.ErrorMessage)
	}
	if row.FinishedAt == nil {
		t.Fatal("完成应有 finished_at")
	}
	logs, err := f.m.ListNodeLogs(ctx, run.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("三个节点应各有一条日志，实际 %d", len(logs))
	}
	// 标签节点真的加了标签。
	after, _ := f.m.GetContact(ctx, contact.ID)
	found := false
	for _, tag := range after.Tags {
		if tag == "greeted" {
			found = true
		}
	}
	if !found {
		t.Fatalf("标签节点没生效: %#v", after.Tags)
	}
}

// TestAutomationIdempotentOnRerun 重复推进不会重复执行动作。
func TestAutomationIdempotentOnRerun(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "idem@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "幂等流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "tag", map[string]any{"add": []any{"once"}}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	// 再推进两次：终态应直接返回，不产生任何新日志。
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	logs, _ := f.m.ListNodeLogs(ctx, run.ID, 100)
	if len(logs) != 3 {
		t.Fatalf("重复推进不该新增日志，实际 %d 条", len(logs))
	}
}

// TestAutomationDelayParksAndResumes 等待节点推进游标后挂起，唤醒时从下一步继续。
func TestAutomationDelayParksAndResumes(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "delay@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "等待流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "delay", map[string]any{"minutes": 60}, "n3"),
		newNode("n3", "tag", map[string]any{"add": []any{"after_wait"}}, "n4"),
		newNode("n4", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	row, _ := f.m.GetRun(ctx, run.ID)
	if row.Status != mailmodel.RunStatusWaiting {
		t.Fatalf("应挂起等待，实际 %s", row.Status)
	}
	if row.NextRunAt == nil {
		t.Fatal("等待应记录 next_run_at")
	}
	// **游标必须已经推进到下一步** —— 否则唤醒后会重新等待一次。
	if row.CurrentNode == nil || *row.CurrentNode != "n3" {
		t.Fatalf("等待后游标应在 n3，实际 %v", row.CurrentNode)
	}
	// 唤醒：继续走完。
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	row, _ = f.m.GetRun(ctx, run.ID)
	if row.Status != mailmodel.RunStatusCompleted {
		t.Fatalf("唤醒后应完成，实际 %s", row.Status)
	}
	after, _ := f.m.GetContact(ctx, contact.ID)
	found := false
	for _, tag := range after.Tags {
		if tag == "after_wait" {
			found = true
		}
	}
	if !found {
		t.Fatalf("唤醒后应执行 n3 的标签动作: %#v", after.Tags)
	}
}

// TestAutomationBranchOnTag 条件分支按标签走对应分支。
func TestAutomationBranchOnTag(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	plain := &mailmodel.MailContactEntity{
		Email: "plain@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	vip := &mailmodel.MailContactEntity{
		Email: "vip@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
		Tags: mailmodel.StringArray{"vip"},
	}
	if err := f.m.CreateContact(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if err := f.m.CreateContact(ctx, vip); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "分支流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		map[string]any{
			"key": "n2", "type": "branch",
			"params": map[string]any{"conditions": []any{"has_tag:vip"}},
			"yes":    "n3", "no": "n4",
		},
		newNode("n3", "tag", map[string]any{"add": []any{"got_vip_path"}}, "n5"),
		newNode("n4", "tag", map[string]any{"add": []any{"got_plain_path"}}, "n5"),
		newNode("n5", "end", nil, ""),
	}, mailmodel.TriggerManual)

	for _, c := range []*mailmodel.MailContactEntity{plain, vip} {
		if _, err := f.svc.StartRun(ctx, id, c.ID, mailmodel.TriggerManual); err != nil {
			t.Fatal(err)
		}
		run, _ := f.m.ActiveRun(ctx, id, c.ID)
		if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
	}
	check := func(c *mailmodel.MailContactEntity, want string) {
		t.Helper()
		row, _ := f.m.GetContact(ctx, c.ID)
		for _, tag := range row.Tags {
			if tag == want {
				return
			}
		}
		t.Fatalf("%s 应有标签 %s，实际 %#v", c.Email, want, row.Tags)
	}
	check(plain, "got_plain_path")
	check(vip, "got_vip_path")
}

// TestAutomationSuppressionSkipsSend 抑制名单里的地址：发信节点记为跳过，流程继续走完。
func TestAutomationSuppressionSkipsSend(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "blocked@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	if err := f.m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
		Email: contact.Email, Reason: mailmodel.SuppressionReasonUnsubscribe,
	}); err != nil {
		t.Fatal(err)
	}
	// 模板与账号都要有，否则走不到「抑制检查」之后的路径；这里只验证抑制这条。
	if _, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "auto_tpl", Subject: "s", BodyHTML: "<p>x</p>",
	}); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "抑制流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "email", map[string]any{"template_key": "auto_tpl"}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if _, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, _ := f.m.ActiveRun(ctx, id, contact.ID)
	if err := f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进失败: %v", err)
	}
	row, _ := f.m.GetRun(ctx, run.ID)
	if row.Status != mailmodel.RunStatusCompleted {
		t.Fatalf("抑制住的地址不该让流程失败，实际 %s（%v）", row.Status, row.ErrorMessage)
	}
	logs, _ := f.m.ListNodeLogs(ctx, run.ID, 10)
	foundSkip := false
	for _, l := range logs {
		// Detail 与 Explain 同源，都是**运行文案编码**（service 层拿不到请求语言），出口用
		// mailenums.FormatRunText 还原。断言必须走还原，否则测的是中间产物。
		// 取词用「只回兜底」的函数：断言代码内兜底文案，不依赖测试库词条质量。
		if l.NodeKey != "n2" || l.Detail == nil {
			continue
		}
		detail := mailenums.FormatRunText(func(_, fallback string) string { return fallback }, *l.Detail)
		if strings.Contains(detail, "抑制名单") {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Fatal("发信节点的日志应说明因抑制名单未发送")
	}
}

// TestStartRunIsIdempotentWithinFlow 同一人在同一流程里不会有两个进行中实例。
func TestStartRunIsIdempotentWithinFlow(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "dup@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	id := seedAutomation(t, f, "防重复流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "delay", map[string]any{"minutes": 30}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)
	first, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual)
	if err != nil || !first {
		t.Fatalf("首次应启动成功 started=%v err=%v", first, err)
	}
	second, err := f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("已在流程中不该重复启动（否则同一人会收到多轮邮件）")
	}
	_, total, err := f.m.ListRuns(ctx, id, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("应只有 1 个实例，实际 %d", total)
	}
}
