package adminservice

import (
	"cmp"
	"slices"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
)

func permMenu(id, parent uint64, typ int, code string, status int) adminmodel.MenuEntity {
	m := adminmodel.MenuEntity{ID: id, ParentID: parent, Type: typ, Status: status, Title: "m"}
	if code != "" {
		c := code
		m.PermissionCode = &c
	}
	return m
}

func sameIDs(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestWithAncestorMenuIDsCompletesParents(t *testing.T) {
	all := []adminmodel.MenuEntity{
		permMenu(10, 0, adminmodel.MenuTypeDirectory, "", 1),
		permMenu(20, 10, adminmodel.MenuTypeMenu, "page:list", 1),
		permMenu(30, 20, adminmodel.MenuTypeButton, "page:delete", 1),
	}

	got := withAncestorMenuIDs(all, []uint64{30})
	if !sameIDs(got, []uint64{10, 20, 30}) {
		t.Fatalf("祖先补齐不符: got=%v want=[10 20 30]", got)
	}

	got = withAncestorMenuIDs(all, []uint64{20, 10})
	if !sameIDs(got, []uint64{10, 20}) {
		t.Fatalf("多入口去重/排序不符: got=%v", got)
	}

	if got = withAncestorMenuIDs(all, nil); got != nil {
		t.Fatalf("空输入应返回 nil: got=%v", got)
	}
}

func TestWithAncestorMenuIDsDropsUnknownIDs(t *testing.T) {
	all := []adminmodel.MenuEntity{
		permMenu(10, 0, adminmodel.MenuTypeDirectory, "", 1),
		permMenu(20, 10, adminmodel.MenuTypeMenu, "page:list", 1),
	}

	got := withAncestorMenuIDs(all, []uint64{999, 20, 999})
	if !sameIDs(got, []uint64{10, 20}) {
		t.Fatalf("未知 id 应被丢弃: got=%v", got)
	}
}

func TestWithAncestorMenuIDsSurvivesCycle(t *testing.T) {

	all := []adminmodel.MenuEntity{
		permMenu(10, 20, adminmodel.MenuTypeDirectory, "", 1),
		permMenu(20, 10, adminmodel.MenuTypeMenu, "page:list", 1),
	}

	done := make(chan []uint64, 1)
	go func() { done <- withAncestorMenuIDs(all, []uint64{20}) }()
	select {
	case got := <-done:
		if !sameIDs(got, []uint64{10, 20}) {
			t.Fatalf("成环时的补齐结果不符: got=%v", got)
		}
	case <-time.After(2 * time.Second):

		t.Fatal("parent_id 成环时上溯没有终止（死循环）")
	}
}

func TestBuildPermissionTreeKeepsCheckedDisabledNode(t *testing.T) {
	all := []adminmodel.MenuEntity{
		permMenu(10, 0, adminmodel.MenuTypeDirectory, "", 1),
		permMenu(20, 10, adminmodel.MenuTypeMenu, "page:list", adminmodel.MenuStatusDisabled),
		permMenu(30, 20, adminmodel.MenuTypeButton, "page:delete", 1),
		permMenu(40, 0, adminmodel.MenuTypeMenu, "other:list", adminmodel.MenuStatusDisabled),
	}

	tree := buildPermissionTree(all, []uint64{30})
	got := collectTreeIDs(tree)
	slices.SortFunc(got, func(a, b uint64) int { return cmp.Compare(a, b) })

	want := []uint64{10, 20, 30}
	if !sameIDs(got, want) {
		t.Fatalf("分配树节点不符: got=%v want=%v", got, want)
	}

	node := findTreeNode(tree, 30)
	if node == nil {
		t.Fatal("分配树里找不到已勾选的按钮节点")
	}
	if node.ParentID != 20 {
		t.Fatalf("按钮节点的父 id 不符: got=%d want=20", node.ParentID)
	}
}

func collectTreeIDs(nodes []admindto.MenuTreeNode) []uint64 {
	var out []uint64
	for _, n := range nodes {
		out = append(out, n.ID)
		out = append(out, collectTreeIDs(n.Children)...)
	}
	return out
}

func findTreeNode(nodes []admindto.MenuTreeNode, id uint64) *admindto.MenuTreeNode {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if found := findTreeNode(nodes[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}
