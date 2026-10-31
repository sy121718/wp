package orderservice

import (
	"testing"

	ordermodel "go_wp/internal/module/order/model"
)

var documentedEdges = map[string][]string{
	ordermodel.OrderStatusPending:   {ordermodel.OrderStatusPaid, ordermodel.OrderStatusCancelled},
	ordermodel.OrderStatusPaid:      {ordermodel.OrderStatusShipped, ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded},
	ordermodel.OrderStatusShipped:   {ordermodel.OrderStatusCompleted, ordermodel.OrderStatusRefunded},
	ordermodel.OrderStatusCompleted: {ordermodel.OrderStatusRefunded},
	ordermodel.OrderStatusCancelled: {},
	ordermodel.OrderStatusRefunded:  {},
}

var allStatuses = []string{
	ordermodel.OrderStatusPending, ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped,
	ordermodel.OrderStatusCompleted, ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded,
}

func TestTransitionTableMatchesDocumentedEdges(t *testing.T) {
	for _, from := range allStatuses {
		want := map[string]bool{}
		for _, to := range documentedEdges[from] {
			want[to] = true
		}
		for _, to := range allStatuses {
			got := canTransition(from, to)
			if got != want[to] {
				if want[to] {
					t.Errorf("合法边被拒绝: %s → %s", from, to)
				} else {
					t.Errorf("发现了文档未声明的边: %s → %s（多余的一条边就是一条不该存在的路径）", from, to)
				}
			}
		}
	}
}

func TestTerminalStatesHaveNoOutgoingEdges(t *testing.T) {
	for _, terminal := range []string{ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded} {
		edges, ok := allowedTransitions[terminal]
		if !ok {
			t.Fatalf("终态 %s 必须显式出现在表里（空出边），缺条目与「不允许任何流转」看起来一样但含义不同", terminal)
		}
		if len(edges) != 0 {
			t.Errorf("终态 %s 不应有出边，实际 %v", terminal, edges)
		}
		for _, to := range allStatuses {
			if canTransition(terminal, to) {
				t.Errorf("终态 %s 不应能流转到 %s", terminal, to)
			}
		}
	}
}

func TestCanTransitionRejectsUnknownAndSelfLoop(t *testing.T) {
	if canTransition("not-a-status", ordermodel.OrderStatusPaid) {
		t.Error("未知来源状态不应合法")
	}
	if canTransition(ordermodel.OrderStatusPending, "not-a-status") {
		t.Error("未知目标状态不应合法")
	}
	if canTransition("", ordermodel.OrderStatusPaid) {
		t.Error("空来源状态不应合法")
	}
	for _, s := range allStatuses {
		if canTransition(s, s) {
			t.Errorf("自环必须拒绝: %s → %s（重复流转会多记一条流水，且掩盖并发问题）", s, s)
		}
	}
}

func TestIsKnownStatus(t *testing.T) {
	for _, s := range allStatuses {
		if !isKnownStatus(s) {
			t.Errorf("%s 应为已知状态", s)
		}
	}
	for _, s := range []string{"", "PAID", "paid ", "unknown"} {
		if isKnownStatus(s) {
			t.Errorf("%q 不应被判为已知状态（大小写与空白都不该被默默容忍）", s)
		}
	}

	if len(allowedTransitions) != len(allStatuses) {
		t.Fatalf("状态机表的条目数(%d)与状态集合(%d)不一致", len(allowedTransitions), len(allStatuses))
	}
	for from, edges := range allowedTransitions {
		if !isKnownStatus(from) {
			t.Errorf("表里的来源状态 %s 不是已知状态", from)
		}
		for to := range edges {
			if !isKnownStatus(to) {
				t.Errorf("表里的目标状态 %s 不是已知状态", to)
			}
		}
	}
}
