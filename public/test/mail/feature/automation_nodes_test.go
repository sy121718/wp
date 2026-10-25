package feature

// automation_nodes_test.go — T4 四个新节点的执行判据（真实 PG）。
//
// 每个用例都跑「建流程 → 启用 → 建实例 → 推进到底」，断言的是**执行结果**而不是
// 校验器能不能拒：
//
//   · list_status_branch —— 三档状态各走各的出口；
//   · ab_test            —— 同一实例重复推进落在同一个变体（幂等 + 确定性）；
//   · webhook            —— 内网地址在执行期被拒，但流程**继续**走到结束；
//   · filter 的 mode=any —— 任一条件命中就走 yes（v1 只有 AND 语义）。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// seedAutomationForMail 用 mailFixture 建一条启用中的流程（seedAutomation 收的是 *fixture）。
func seedAutomationForMail(t *testing.T, f *mailFixture, name string, nodes []map[string]any) uint64 {
	t.Helper()
	list := make([]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	raw, err := json.Marshal(map[string]any{"schema": 2, "entry": "n1", "nodes": list})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	item, err := f.svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name: name, TriggerType: mailmodel.TriggerManual, Definition: raw,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	if err := f.svc.SetAutomationStatus(ctx, &maildto.SetAutomationStatusReq{
		ID: item.ID, Status: mailmodel.AutomationStatusActive,
	}); err != nil {
		t.Fatalf("启用流程失败: %v", err)
	}
	return item.ID
}

// createDraftAutomation 建一条**草稿**流程（不启用），返回 id。
//
// 与 seedAutomationForMail 的分工：那个建完就启用，而这个留给用例自己决定何时启用
// （需要「草稿期间被改坏」的场景）。
func createDraftAutomation(t *testing.T, f *mailFixture, name string, nodes []map[string]any) uint64 {
	t.Helper()
	list := make([]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	raw, err := json.Marshal(map[string]any{"schema": 2, "entry": "n1", "nodes": list})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.svc.SaveAutomation(context.Background(), &maildto.SaveAutomationReq{
		Name: name, TriggerType: mailmodel.TriggerManual, Definition: raw,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	return item.ID
}

// runAutomationToEnd 建实例并推进到底，返回实例 id 与联系人。
func runAutomationToEnd(t *testing.T, f *mailFixture, automationID uint64, email string) (uint64, *mailmodel.MailContactEntity) {
	t.Helper()
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	contact, err := m.GetContactByEmail(ctx, email)
	if err != nil {
		t.Fatalf("取联系人失败: %v", err)
	}
	if _, err = f.svc.StartRun(ctx, automationID, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatalf("建实例失败: %v", err)
	}
	run, err := m.ActiveRun(ctx, automationID, contact.ID)
	if err != nil {
		t.Fatalf("取实例失败: %v", err)
	}
	if err = f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进失败: %v", err)
	}
	return run.ID, contact
}

// logDetailOf 取某实例某节点的日志明文（含运行文案 key，便于断言分支）。
func logDetailOf(t *testing.T, f *mailFixture, runID uint64, nodeKey string) string {
	t.Helper()
	logs, err := mailmodel.NewMailModel(f.db).ListNodeLogs(context.Background(), runID, 100)
	if err != nil {
		t.Fatalf("取节点日志失败: %v", err)
	}
	for _, l := range logs {
		if l.NodeKey != nodeKey {
			continue
		}
		if l.Detail == nil {
			return ""
		}
		return *l.Detail
	}
	return ""
}

// TestListStatusBranchRoutesByStatus 三档状态各走各的出口。
func TestListStatusBranchRoutesByStatus(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 流程：触发 → 按状态分流 →（三个出口各打不同标签）→ 结束。
	// 三个标签就是「走了哪条出口」的可观察结果。
	id := seedAutomationForMail(t, f, "按状态分流", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		{
			"key": "n2", "type": "list_status_branch",
			"out": map[string]any{"not_in_list": "n5", "active": "n3", "non_active": "n4"},
		},
		newNode("n3", "tag", map[string]any{"add": []any{"went_active"}}, "n6"),
		newNode("n4", "tag", map[string]any{"add": []any{"went_non_active"}}, "n6"),
		newNode("n5", "tag", map[string]any{"add": []any{"went_not_in_list"}}, "n6"),
		newNode("n6", "end", nil, ""),
	})

	cases := []struct {
		email  string
		status string
		want   string
	}{
		{"active@example.com", mailmodel.ContactStatusSubscribed, "went_active"},
		{"pending@example.com", mailmodel.ContactStatusPending, "went_non_active"},
		{"unsub@example.com", mailmodel.ContactStatusUnsubscribed, "went_not_in_list"},
	}
	for _, c := range cases {
		if err := f.seedContact(ctx, c.email, "x", c.status, ""); err != nil {
			t.Fatalf("建联系人失败: %v", err)
		}
		_, contact := runAutomationToEnd(t, f, id, c.email)
		after, err := mailmodel.NewMailModel(f.db).GetContact(ctx, contact.ID)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(after.Tags, ",")
		if !strings.Contains(joined, c.want) {
			t.Fatalf("状态 %s 应走 %s 分支，实际标签 %v", c.status, c.want, after.Tags)
		}
	}
}

// TestABTestIsDeterministicPerRun 同一个实例推进两次落在同一个变体（幂等 + 确定性）。
func TestABTestIsDeterministicPerRun(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedAutomationForMail(t, f, "A/B 分流", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		{
			"key": "n2", "type": "ab_test",
			"params": map[string]any{"variants": []any{
				map[string]any{"id": "v1", "name": "A", "weight": float64(50)},
				map[string]any{"id": "v2", "name": "B", "weight": float64(50)},
			}},
			"out": map[string]any{"v1": "n3", "v2": "n4"},
		},
		newNode("n3", "tag", map[string]any{"add": []any{"variant_a"}}, "n5"),
		newNode("n4", "tag", map[string]any{"add": []any{"variant_b"}}, "n5"),
		newNode("n5", "end", nil, ""),
	})

	if err := f.seedContact(ctx, "ab@example.com", "ab", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	runID, contact := runAutomationToEnd(t, f, id, "ab@example.com")

	detail := logDetailOf(t, f, runID, "n2")
	if detail == "" {
		t.Fatal("A/B 节点应写一条日志（命中哪个变体是排障的第一个问题）")
	}
	if !strings.Contains(detail, mailenums.RunKeyABVariant) {
		t.Fatalf("A/B 日志应是 %s 编码，实际 %s", mailenums.RunKeyABVariant, detail)
	}

	after, err := mailmodel.NewMailModel(f.db).GetContact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(after.Tags, ",")
	hitA := strings.Contains(joined, "variant_a")
	hitB := strings.Contains(joined, "variant_b")
	if hitA == hitB {
		t.Fatalf("应恰好命中一个变体，实际标签 %v", after.Tags)
	}

	// 重跑同一个实例：节点日志已 ok，动作不该重复执行（幂等），变体也不该换。
	if err := f.svc.RunAutomation(ctx, runID); err != nil {
		t.Fatalf("重复推进应无错: %v", err)
	}
	again, _ := mailmodel.NewMailModel(f.db).GetContact(ctx, contact.ID)
	if got := strings.Join(again.Tags, ","); got != joined {
		t.Fatalf("重复推进换了分支：%q → %q", joined, got)
	}
}

// TestWebhookFailureFailsNodeButFlowContinues 回调失败不终止流程。
//
// 这是四个新节点里唯一一个「失败也要继续」的语义：第三方暂时 500 不该吃掉用户后面
// 那封邮件。判据分两段：该节点日志是 failed（排障看得到），但**流程走到了结束**。
//
// 失败用注入的 HTTP 客户端造（SetWebhookClient）：真实网络不可控，而「回调 500」
// 恰恰是这条分支要覆盖的形态。SSRF 的拒绝路径由 webhook_ssrf_test.go 单测覆盖 ——
// 那条路径同样是 failed + 继续，只是原因不同。
func TestWebhookFailureFailsNodeButFlowContinues(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 注入一个恒定返回 500 的客户端（不发真请求）。
	old := f.svc
	f.svc.SetWebhookClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("boom")),
			Header:     make(http.Header),
		}, nil
	})})
	t.Cleanup(func() { old.SetWebhookClient(nil) })

	id := seedAutomationForMail(t, f, "回调节点", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		{
			"key": "n2", "type": "webhook",
			"params": map[string]any{"url": "https://93.184.216.34/hook"},
			"out":    map[string]any{"next": "n3"},
		},
		newNode("n3", "tag", map[string]any{"add": []any{"after_webhook"}}, "n4"),
		newNode("n4", "end", nil, ""),
	})

	if err := f.seedContact(ctx, "hook@example.com", "hook", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	runID, contact := runAutomationToEnd(t, f, id, "hook@example.com")

	// ① 回调节点记 failed（排障看得到「回调没成功」）。
	logs, err := mailmodel.NewMailModel(f.db).ListNodeLogs(ctx, runID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var hookStatus string
	for _, l := range logs {
		if l.NodeKey == "n2" {
			hookStatus = l.Status
		}
	}
	if hookStatus != mailmodel.NodeStatusFailed {
		t.Fatalf("回调 500 应记 failed，实际 %q", hookStatus)
	}

	// ② 流程继续：下游标签节点执行了，实例 completed。
	after, err := mailmodel.NewMailModel(f.db).GetContact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(after.Tags, ","), "after_webhook") {
		t.Fatalf("回调失败不该终止流程，下游标签没执行：%v", after.Tags)
	}
	run, err := mailmodel.NewMailModel(f.db).GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != mailmodel.RunStatusCompleted {
		t.Fatalf("回调失败后流程应走完，实例状态实际 %q", run.Status)
	}
}

// roundTripFunc 让函数满足 http.RoundTripper（注入假响应）。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestFilterModeAnyTakesYes 任一条件命中就走 yes（v1 只有 AND 语义）。
func TestFilterModeAnyTakesYes(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedAutomationForMail(t, f, "任一条件", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		{
			"key": "n2", "type": "filter",
			"params": map[string]any{
				// closed 条件不满足（没打开过），opened 也不满足 —— 但 subscribed 满足。
				"mode":       "any",
				"conditions": []any{"opened", "subscribed"},
			},
			"out": map[string]any{"yes": "n3", "no": "n4"},
		},
		newNode("n3", "tag", map[string]any{"add": []any{"went_yes"}}, "n5"),
		newNode("n4", "tag", map[string]any{"add": []any{"went_no"}}, "n5"),
		newNode("n5", "end", nil, ""),
	})

	if err := f.seedContact(ctx, "any@example.com", "any", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	_, contact := runAutomationToEnd(t, f, id, "any@example.com")
	after, err := mailmodel.NewMailModel(f.db).GetContact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(after.Tags, ",")
	if !strings.Contains(joined, "went_yes") {
		t.Fatalf("mode=any 时任一条件满足应走 yes，实际标签 %v", after.Tags)
	}
}

// TestSaveRejectsBadABTestWeights 保存时就拒绝权重和不对的分流转（不是执行时才炸）。
func TestSaveRejectsBadABTestWeights(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	raw, merr := json.Marshal(map[string]any{"schema": 2, "entry": "n1", "nodes": []any{
		map[string]any{"key": "n1", "type": "trigger", "out": map[string]any{"next": "n2"}},
		map[string]any{"key": "n2", "type": "ab_test",
			"params": map[string]any{"variants": []any{
				map[string]any{"id": "v1", "weight": 30},
				map[string]any{"id": "v2", "weight": 30},
			}},
			"out": map[string]any{"v1": "n3", "v2": "n3"}},
		map[string]any{"key": "n3", "type": "end", "out": map[string]any{}},
	}})
	if merr != nil {
		t.Fatal(merr)
	}
	_, err := f.svc.SaveAutomation(context.Background(), &maildto.SaveAutomationReq{
		Name: "坏权重", TriggerType: mailmodel.TriggerManual, Definition: raw,
	})
	if err == nil {
		t.Fatal("权重和 60 的分流转应被拒绝")
	}
	if !strings.Contains(err.Error(), mailenums.DetailGraphVariantWeightBad) {
		t.Fatalf("错误应带 detail key %s，实际 %v", mailenums.DetailGraphVariantWeightBad, err)
	}
}
