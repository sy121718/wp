package orderservice

// order_status_test.go — 状态机的就近单测（审计 CQ-020）。
//
// 为什么放在模块内而不是 public/test：这是一张**纯逻辑表**，不需要数据库、
// 不需要装配，放模块内改一次跑一次的成本几乎为零；放 feature 层则要先起库，
// 于是「加了一条边忘了同步判断」这种改动反而最不容易被跑到的测试覆盖。
//
// 核心断言是**穷举**：6 个状态的全部有序组合逐一比对，而不是只验证文档里
// 写出来的那几条边。只验证已知边的话，多出来一条边不会有任何测试失败 ——
// 而多余的一条边就是一条不该存在的业务路径（例如 completed → cancelled）。

import (
	"testing"

	ordermodel "go_wp/internal/module/order/model"
)

// documentedEdges 与 order.go 里状态机注释逐条对应的合法边。
var documentedEdges = map[string][]string{
	ordermodel.OrderStatusPending:   {ordermodel.OrderStatusPaid, ordermodel.OrderStatusCancelled},
	ordermodel.OrderStatusPaid:      {ordermodel.OrderStatusShipped, ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded},
	ordermodel.OrderStatusShipped:   {ordermodel.OrderStatusCompleted, ordermodel.OrderStatusRefunded},
	ordermodel.OrderStatusCompleted: {ordermodel.OrderStatusRefunded},
	ordermodel.OrderStatusCancelled: {},
	ordermodel.OrderStatusRefunded:  {},
}

// allStatuses 全部状态（顺序与 isKnownStatus 内的列表一致）。
var allStatuses = []string{
	ordermodel.OrderStatusPending, ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped,
	ordermodel.OrderStatusCompleted, ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded,
}

// TestTransitionTableMatchesDocumentedEdges 穷举 6×6 组合，表里只允许出现文档写明的边。
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

// TestTerminalStatesHaveNoOutgoingEdges 终态没有出边。
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

// TestCanTransitionRejectsUnknownAndSelfLoop 未知状态与自环一律拒绝。
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

// TestIsKnownStatus 状态集合与表覆盖的状态一致。
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
	// 表里的状态必须全部是已知状态，反之亦然：两处漂移会让某条边永远走不通。
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
