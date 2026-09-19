package adminservice

import (
	"sort"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
)

// role_permission_test.go — 角色权限分配里两段纯逻辑的回归：祖先补齐与分配树构建。
//
// 这两段都不是「顺序无关的辅助函数」：
//   - withAncestorMenuIDs 决定「子勾 ⇒ 父勾」这条不变式是否成立。不成立时角色会拿到
//     「接口能调、侧栏没有入口」的分裂授权（按钮的权限点在 Casbin 里通，父菜单的码不在
//     codes 里，于是 BuildAuthorizedTree 不生成入口）；
//   - buildPermissionTree 决定已勾选的禁用节点会不会从树里消失。消失就是静默丢授权：
//     页面渲染不出这个勾选，管理员一保存就把这条策略删掉了。

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

	// 只勾按钮：目录（无权限码、反查永远查不到）与父菜单都必须被补齐。
	got := withAncestorMenuIDs(all, []uint64{30})
	if !sameIDs(got, []uint64{10, 20, 30}) {
		t.Fatalf("祖先补齐不符: got=%v want=[10 20 30]", got)
	}

	// 已是根的节点不需要补，结果同时是升序（页面与接口都依赖确定性顺序）。
	got = withAncestorMenuIDs(all, []uint64{20, 10})
	if !sameIDs(got, []uint64{10, 20}) {
		t.Fatalf("多入口去重/排序不符: got=%v", got)
	}

	// 空输入返回 nil（不是空切片），调用方据此短路。
	if got = withAncestorMenuIDs(all, nil); got != nil {
		t.Fatalf("空输入应返回 nil: got=%v", got)
	}
}

func TestWithAncestorMenuIDsDropsUnknownIDs(t *testing.T) {
	all := []adminmodel.MenuEntity{
		permMenu(10, 0, adminmodel.MenuTypeDirectory, "", 1),
		permMenu(20, 10, adminmodel.MenuTypeMenu, "page:list", 1),
	}

	// 不存在的 id 被丢弃：这同时是保存入口的白名单 —— 请求体里的 id 不进 SQL。
	got := withAncestorMenuIDs(all, []uint64{999, 20, 999})
	if !sameIDs(got, []uint64{10, 20}) {
		t.Fatalf("未知 id 应被丢弃: got=%v", got)
	}
}

func TestWithAncestorMenuIDsSurvivesCycle(t *testing.T) {
	// 脏数据成环（10 → 20 → 10）。上溯必须终止：这里失败的表现是请求把进程挂死。
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
		// 上溯不自带深度上限（靠 seen 防环）：这条断言是「seen 真的生效了」的唯一防线，
		// 去掉它，成环的脏数据会让任何一个打开分配页的请求把 goroutine 卡死。
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
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })

	// 20 已禁用但它是已勾选按钮 30 的祖先 —— 必须保留，否则下次保存时 30 的授权会连带消失。
	// 40 未勾选且已禁用 —— 不进树（页面不该让人去勾一个已经关掉的菜单）。
	want := []uint64{10, 20, 30}
	if !sameIDs(got, want) {
		t.Fatalf("分配树节点不符: got=%v want=%v", got, want)
	}

	// 祖先关系必须真的接上了：扁平行渲染与 JS 联动都靠 data-parent，而折叠靠 DFS 连续性。
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
