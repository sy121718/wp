package feature

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

func TestListStatusBranchRoutesByStatus(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

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

	if err := f.svc.RunAutomation(ctx, runID); err != nil {
		t.Fatalf("重复推进应无错: %v", err)
	}
	again, _ := mailmodel.NewMailModel(f.db).GetContact(ctx, contact.ID)
	if got := strings.Join(again.Tags, ","); got != joined {
		t.Fatalf("重复推进换了分支：%q → %q", joined, got)
	}
}

func TestWebhookFailureFailsNodeButFlowContinues(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
