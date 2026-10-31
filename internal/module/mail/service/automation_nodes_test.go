package mailservice

import (
	"strings"
	"testing"

	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

func TestBucketOfIsDeterministic(t *testing.T) {
	first := bucketOf(4242, "ab1")
	for i := 0; i < 1000; i++ {
		if got := bucketOf(4242, "ab1"); got != first {
			t.Fatalf("第 %d 次得到 %d，首次是 %d —— 分桶不确定，重试会换变体", i, got, first)
		}
	}
}

func TestBucketOfSpreadsAcrossRuns(t *testing.T) {
	counts := map[int]int{}
	for runID := uint64(1); runID <= 200; runID++ {
		counts[int(bucketOf(runID, "ab1")%100)]++
	}
	if len(counts) < 2 {
		t.Fatalf("200 个 runID 只落进 %d 个桶 —— 分桶退化成常量了", len(counts))
	}

	v := abVariant{ID: "v1", Weight: 50}
	sideA := 0
	for runID := uint64(1); runID <= 200; runID++ {
		if int(bucketOf(runID, "ab1")%100) < v.Weight {
			sideA++
		}
	}
	if sideA == 0 || sideA == 200 {
		t.Fatalf("200 个样本全部落在同一边（%d/200）—— A/B 分不出来", sideA)
	}
}

func TestBucketOfDependsOnNodeKey(t *testing.T) {
	if bucketOf(1, "ab1") == bucketOf(1, "ab2") && bucketOf(1, "ab1") == bucketOf(1, "ab3") {
		t.Fatal("不同节点 key 得到同一桶 —— 一个用户在所有 A/B 节点上被绑到同一组")
	}
}

func TestABOutletOfPicksByWeight(t *testing.T) {
	node := AutomationNode{
		Key: "ab1", Type: NodeTypeABTest,
		Params: map[string]any{"variants": []any{
			map[string]any{"id": "v1", "weight": float64(50)},
			map[string]any{"id": "v2", "weight": float64(50)},
		}},
		Out: map[string]string{"v1": "n2", "v2": "n3"},
	}
	saw := map[string]bool{}
	for runID := uint64(1); runID <= 200; runID++ {
		outlet, err := abOutletOf(runID, node)
		if err != nil {
			t.Fatalf("runID %d 取变体失败: %v", runID, err)
		}
		if outlet != "v1" && outlet != "v2" {
			t.Fatalf("命中未知变体 %q", outlet)
		}
		saw[outlet] = true
	}
	if len(saw) != 2 {
		t.Fatalf("200 个样本只命中 %d 个变体", len(saw))
	}
}

func TestABOutletOfFailsWhenOutletMissing(t *testing.T) {
	node := AutomationNode{
		Key: "ab1", Type: NodeTypeABTest,
		Params: map[string]any{"variants": []any{
			map[string]any{"id": "v1", "weight": float64(100)},
			map[string]any{"id": "v2", "weight": float64(0)},
		}},
		Out: map[string]string{"v1": "n2"},
	}

	if _, err := abOutletOf(7, node); err != nil {
		t.Fatalf("v1 有出口时不该报错: %v", err)
	}

	node.Out = map[string]string{}
	if _, err := abOutletOf(7, node); err == nil {
		t.Fatal("命中变体没有出口时必须报错，不能静默走第一个")
	}
}

func TestListStatusOutletMapping(t *testing.T) {
	cases := map[string]string{
		mailmodel.ContactStatusSubscribed:   outletActive,
		mailmodel.ContactStatusPending:      outletNonActive,
		mailmodel.ContactStatusUnsubscribed: outletNotInList,
		"":                                  outletNotInList,
		"weird-status":                      outletNotInList,
	}
	for status, want := range cases {
		if got := listStatusOutlet(status); got != want {
			t.Errorf("状态 %q → %q，期望 %q", status, got, want)
		}
	}
}

func TestNodeArityRejectsBadNewNodes(t *testing.T) {
	cases := []struct {
		name string
		node AutomationNode
		want string
	}{
		{
			name: "list_status_branch 缺出口",
			node: AutomationNode{Key: "s1", Type: NodeTypeListStatusBranch,
				Out: map[string]string{"active": "n2", "non_active": "n3"}},
			want: "缺少出口",
		},
		{
			name: "ab_test 变体不足两个",
			node: AutomationNode{Key: "ab1", Type: NodeTypeABTest,
				Params: map[string]any{"variants": []any{
					map[string]any{"id": "v1", "weight": float64(100)},
				}},
				Out: map[string]string{"v1": "n2"}},
			want: "2–5 个变体",
		},
		{
			name: "ab_test 权重和不是 100",
			node: AutomationNode{Key: "ab1", Type: NodeTypeABTest,
				Params: map[string]any{"variants": []any{
					map[string]any{"id": "v1", "weight": float64(30)},
					map[string]any{"id": "v2", "weight": float64(30)},
				}},
				Out: map[string]string{"v1": "n2", "v2": "n3"}},
			want: "权重之和必须是 100",
		},
		{
			name: "ab_test 变体 id 重复",
			node: AutomationNode{Key: "ab1", Type: NodeTypeABTest,
				Params: map[string]any{"variants": []any{
					map[string]any{"id": "v1", "weight": float64(50)},
					map[string]any{"id": "v1", "weight": float64(50)},
				}},
				Out: map[string]string{"v1": "n2"}},
			want: "变体 id 重复",
		},
		{
			name: "webhook 缺 url",
			node: AutomationNode{Key: "w1", Type: NodeTypeWebhook,
				Params: map[string]any{}, Out: map[string]string{"next": "n2"}},
			want: "需要 https 地址",
		},
		{
			name: "webhook 指向内网",
			node: AutomationNode{Key: "w1", Type: NodeTypeWebhook,
				Params: map[string]any{"url": "https://10.0.0.5/hook"}, Out: map[string]string{"next": "n2"}},
			want: "不允许指向内网",
		},
		{
			name: "webhook 用 http（非 https）",
			node: AutomationNode{Key: "w1", Type: NodeTypeWebhook,
				Params: map[string]any{"url": "http://93.184.216.34/hook"}, Out: map[string]string{"next": "n2"}},
			want: "不允许指向内网",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := nodeArity(c.node)
			if err == nil {
				t.Fatalf("应被拒绝（含 %q）", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误应含 %q，实际 %v", c.want, err)
			}
		})
	}
}

func TestNodeArityAcceptsValidNewNodes(t *testing.T) {
	cases := map[string]AutomationNode{
		"list_status_branch": {
			Key: "s1", Type: NodeTypeListStatusBranch,
			Out: map[string]string{"not_in_list": "n1", "active": "n2", "non_active": "n3"},
		},
		"ab_test": {
			Key: "ab1", Type: NodeTypeABTest,
			Params: map[string]any{"variants": []any{
				map[string]any{"id": "v1", "weight": float64(50)},
				map[string]any{"id": "v2", "weight": float64(50)},
			}},
			Out: map[string]string{"v1": "n2", "v2": "n3"},
		},
		"webhook": {
			Key: "w1", Type: NodeTypeWebhook,
			Params: map[string]any{"url": "https://93.184.216.34/hook"},
			Out:    map[string]string{"next": "n2"},
		},
		"filter": {
			Key: "f1", Type: NodeTypeFilter,
			Params: map[string]any{"mode": ConditionModeAll, "conditions": []any{"opened"}},
			Out:    map[string]string{"yes": "n2", "no": "n3"},
		},
	}
	for name, node := range cases {
		if _, err := nodeArity(node); err != nil {
			t.Errorf("%s 的合法形态不该报错: %v", name, err)
		}
	}
}

func TestDetailKeysRegisteredForNewNodes(t *testing.T) {
	registered := map[string]bool{}
	for _, k := range mailenums.DetailKeys {
		registered[k] = true
	}
	for _, k := range []string{
		"admin.mail.detail.outletMissing",
		"admin.mail.detail.needVariants",
		"admin.mail.detail.variantDuplicate",
		"admin.mail.detail.variantWeightBad",
		"admin.mail.detail.needWebhookURL",
		"admin.mail.detail.webhookDenied",
	} {
		if !registered[k] {
			t.Errorf("detail key %s 没进 enums.DetailKeys：读侧会把它当未登记而丢掉", k)
		}
	}
}
