package tree

// tree_test.go — 树组装的判据。
//
// 重点是**边界语义**而不是「能建出树」：三份原实现在「父不在列表里」「环」「兄弟顺序」
// 这三件事上的处理不一样，合并之后必须有测试把它们钉住，否则下次改动会悄悄退回其中一种。

import (
	"strings"
	"testing"
	"time"
)

// testNode 测试用节点（实现 Node[int] 约束）。
type testNode struct {
	ID       int
	ParentID int
	Name     string
	Children []Node[int]
}

func (n testNode) TreeID() int                    { return n.ID }
func (n testNode) TreeParentID() int              { return n.ParentID }
func (n *testNode) SetTreeChildren(c []Node[int]) { n.Children = c }

// shape 把树摊成 "根 .子 ..孙" 这样的层级串，用于断言形状。
func shape(nodes []*testNode) string {
	var parts []string
	var walk func(ns []Node[int], depth int)
	walk = func(ns []Node[int], depth int) {
		for _, n := range ns {
			tn := n.(*testNode)
			parts = append(parts, strings.Repeat(".", depth)+tn.Name)
			walk(tn.Children, depth+1)
		}
	}
	roots := make([]Node[int], len(nodes))
	for i := range nodes {
		roots[i] = nodes[i]
	}
	walk(roots, 0)
	return strings.Join(parts, " ")
}

// countNodes 统计树里每个 ID 出现的次数（用来证明「不丢节点、不重复挂载」）。
func countNodes(nodes []*testNode) map[int]int {
	seen := map[int]int{}
	var walk func(ns []Node[int])
	walk = func(ns []Node[int]) {
		for _, n := range ns {
			tn := n.(*testNode)
			seen[tn.ID]++
			walk(tn.Children)
		}
	}
	roots := make([]Node[int], len(nodes))
	for i := range nodes {
		roots[i] = nodes[i]
	}
	walk(roots)
	return seen
}

// TestBuildBasicShape 多层、多根、兄弟顺序 = 输入顺序。
func TestBuildBasicShape(t *testing.T) {
	items := []*testNode{
		{ID: 1, ParentID: 0, Name: "a"},
		{ID: 2, ParentID: 1, Name: "a1"},
		{ID: 3, ParentID: 1, Name: "a2"},
		{ID: 4, ParentID: 2, Name: "a1x"},
		{ID: 5, ParentID: 0, Name: "b"},
	}
	got := Build(items)
	if want := "a .a1 ..a1x .a2 b"; shape(got) != want {
		t.Fatalf("树形状 %q，期望 %q", shape(got), want)
	}
}

// TestBuildOrphanBecomesRoot 父不在列表里 → 归入根，不丢节点。
//
// 部门树原实现：`else { roots = append(roots, node) }`；菜单树原实现把孤儿并入 childrenOf[0]。
// 两种做法在这里统一成「归入根」。
func TestBuildOrphanBecomesRoot(t *testing.T) {
	items := []*testNode{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 999, Name: "orphan"},    // 父不存在
		{ID: 3, ParentID: 999, Name: "orphan2"},   // 同一个不存在的父
		{ID: 4, ParentID: 2, Name: "orphanChild"}, // 孤儿的孩子跟着孤儿走
	}
	got := Build(items)
	if len(countNodes(got)) != 4 {
		t.Fatalf("节点不能丢：%v", countNodes(got))
	}
	if want := "root orphan .orphanChild orphan2"; shape(got) != want {
		t.Fatalf("孤儿应归入根层且保持输入顺序，实际 %q，期望 %q", shape(got), want)
	}
}

// TestBuildSelfParentIsRoot 自己指向自己：当根，不死循环。
func TestBuildSelfParentIsRoot(t *testing.T) {
	got := Build([]*testNode{{ID: 1, ParentID: 1, Name: "self"}})
	if len(got) != 1 || got[0].Name != "self" {
		t.Fatalf("自引用节点应作为根返回，实际 %q", shape(got))
	}
}

// TestBuildCycleDoesNotHang 环形父引用必须终止，且每个节点恰好出现一次。
//
// 这是本包存在的**主要理由**：Jet 模板递归渲染环形数据会
// `fatal error: stack overflow`（Go 的 fatal error，recover 抓不住，整个进程退出）。
// 管理员写错父级完全可能，所以 Build 必须挡住 —— 宁可让环节点在顶层露出一次。
func TestBuildCycleDoesNotHang(t *testing.T) {
	cases := []struct {
		name  string
		items []*testNode
	}{
		{
			name: "两节点互指",
			items: []*testNode{
				{ID: 1, ParentID: 2, Name: "a"},
				{ID: 2, ParentID: 1, Name: "b"},
			},
		},
		{
			name: "三节点成环",
			items: []*testNode{
				{ID: 1, ParentID: 2, Name: "a"},
				{ID: 2, ParentID: 3, Name: "b"},
				{ID: 3, ParentID: 1, Name: "c"},
			},
		},
		{
			name: "环外还有正常的树",
			items: []*testNode{
				{ID: 1, ParentID: 0, Name: "root"},
				{ID: 2, ParentID: 1, Name: "kid"},
				{ID: 10, ParentID: 11, Name: "x"},
				{ID: 11, ParentID: 10, Name: "y"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			type result struct{ nodes []*testNode }
			done := make(chan result, 1)
			go func() { done <- result{Build(tc.items)} }()
			var got []*testNode
			select {
			case r := <-done:
				got = r.nodes
			case <-time.After(5 * time.Second):
				t.Fatalf("Build 在环形数据上没返回（死循环）")
			}
			for _, it := range tc.items {
				if n := countNodes(got)[it.ID]; n != 1 {
					t.Fatalf("节点 %d(%s) 出现 %d 次，期望恰好 1 次", it.ID, it.Name, n)
				}
			}
		})
	}
}

// TestDetectCycle 环检测能给出具体的链（供管理界面提示「A 和 B 互为父级」）。
func TestDetectCycle(t *testing.T) {
	cycle := DetectCycle([]*testNode{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 3, Name: "a"},
		{ID: 3, ParentID: 2, Name: "b"},
	})
	if len(cycle) == 0 {
		t.Fatalf("应检出一个环")
	}
	if cycle[0] != cycle[len(cycle)-1] {
		t.Fatalf("环应闭合（首尾同 ID），实际 %v", cycle)
	}
	if len(cycle) != 3 {
		t.Fatalf("两节点环应报出 [x y x]（3 个元素），实际 %v", cycle)
	}
}

// TestDetectCycleNone 没有环时返回空。
func TestDetectCycleNone(t *testing.T) {
	if c := DetectCycle([]*testNode{
		{ID: 1, ParentID: 0, Name: "a"},
		{ID: 2, ParentID: 1, Name: "b"},
		{ID: 3, ParentID: 0, Name: "c"},
	}); len(c) != 0 {
		t.Fatalf("无环数据不该报环：%v", c)
	}
}

// TestBuildEmpty 空输入返回 nil（不是空切片）：调用点据此判断「没有树」。
func TestBuildEmpty(t *testing.T) {
	if got := Build([]*testNode{}); got != nil {
		t.Fatalf("空输入应回 nil，实际 %v", got)
	}
	if got := Build([]*testNode(nil)); got != nil {
		t.Fatalf("nil 输入应回 nil，实际 %v", got)
	}
}

// TestBuildSiblingOrderIsInputOrder 兄弟顺序 = 输入顺序，不做任何排序。
//
// 三个调用点的输入都已按 (sort_order, id) 排好；树若自己再排一遍，
// 就会出现「查询改了排序、页面没变」的错觉。
func TestBuildSiblingOrderIsInputOrder(t *testing.T) {
	got := Build([]*testNode{
		{ID: 10, ParentID: 1, Name: "z"},
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 1, Name: "a"},
	})
	if want := "root .z .a"; shape(got) != want {
		t.Fatalf("兄弟顺序应等于输入顺序，实际 %q，期望 %q", shape(got), want)
	}
}
