package adminservice

// menu_page_tree_test.go — 菜单树状列表三段纯逻辑的回归：建树 / 分页 / 摊平可见性。
//
// 为什么值得单独测：这三段都「不报错，只是结果不同」——
//   - 建树：父不在集合里（孤儿、父被软删）或父子成环的行，挂错地方就从页面上消失，
//     表现为「列表突然短了」，看起来像数据被删；
//   - 摊平：Depth / HasChildren / Hidden / Expanded 是模板缩进与折叠三角的唯一依据，
//     算错一列就是「缩进错层」或「点开一个空三角」；
//   - 搜索态的展开：命中行的祖先不展开，用户搜到了东西却看不见 —— 这是最容易漏的一条。
//
// 模板只渲染不算，所以这些断言就是「页面上会是什么样」的等价物。

import (
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
)

func menuPageTestRow(id, parent uint64, sort int) adminmodel.MenuPageRow {
	return adminmodel.MenuPageRow{MenuEntity: adminmodel.MenuEntity{
		ID: id, ParentID: parent, SortOrder: sort, Title: "m",
	}}
}

// flatRow 是三分量断言的取值形态。
type flatRow struct {
	id       uint64
	depth    int
	hasKids  bool
	hidden   bool
	expanded bool
	matched  bool
}

func flatRowsOf(out []admindto.MenuPageRow) []flatRow {
	got := make([]flatRow, 0, len(out))
	for _, r := range out {
		got = append(got, flatRow{r.ID, r.Depth, r.HasChildren, r.Hidden, r.Expanded, r.Matched})
	}
	return got
}

func assertFlatRows(t *testing.T, out []admindto.MenuPageRow, want []flatRow) {
	t.Helper()
	got := flatRowsOf(out)
	if len(got) != len(want) {
		t.Fatalf("摊平行数应为 %d，实际 %d（前序摊平不该丢行也不该重复）", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行：want %+v，got %+v", i, want[i], got[i])
		}
	}
}

// TestFlattenMenuPageRowsBrowsingCollapsesSubtrees 浏览态：默认只显示最上级，子行全部初始隐藏。
func TestFlattenMenuPageRowsBrowsingCollapsesSubtrees(t *testing.T) {
	rows := []adminmodel.MenuPageRow{
		menuPageTestRow(1, 0, 1),
		menuPageTestRow(2, 1, 2),
		menuPageTestRow(3, 2, 3),
		menuPageTestRow(4, 0, 4),
	}
	out := flattenMenuPageRows(buildMenuPageForest(rows, nil))
	assertFlatRows(t, out, []flatRow{
		{id: 1, depth: 0, hasKids: true},
		{id: 2, depth: 1, hasKids: true, hidden: true},
		{id: 3, depth: 2, hidden: true},
		{id: 4, depth: 0},
	})
}

// TestFlattenMenuPageRowsSearchExpandsAncestors 搜索态：命中路径上的祖先初始展开，命中行露出来。
func TestFlattenMenuPageRowsSearchExpandsAncestors(t *testing.T) {
	rows := []adminmodel.MenuPageRow{
		menuPageTestRow(1, 0, 1),
		menuPageTestRow(2, 1, 2),
		menuPageTestRow(3, 2, 3),
		// 另一棵树同样在本页里，但不在命中路径上：不展开、子孙继续隐藏。
		menuPageTestRow(4, 0, 4),
		menuPageTestRow(5, 4, 5),
	}
	out := flattenMenuPageRows(buildMenuPageForest(rows, map[uint64]bool{3: true}))
	assertFlatRows(t, out, []flatRow{
		{id: 1, depth: 0, hasKids: true, expanded: true},
		{id: 2, depth: 1, hasKids: true, expanded: true},
		// 命中行自己的子行没有被读出来（搜索结果不带命中项子树），所以 HasChildren 为假 ——
		// 模板据此不渲染折叠三角（点开一个什么都没有的三角比没有三角更糟）。
		{id: 3, depth: 2, matched: true},
		{id: 4, depth: 0, hasKids: true},
		{id: 5, depth: 1, hidden: true},
	})
}

// TestBuildMenuPageForestKeepsBadRowsAsRoots 坏数据（孤儿 / 成环 / 自环）一行都不能丢。
func TestBuildMenuPageForestKeepsBadRowsAsRoots(t *testing.T) {
	rows := []adminmodel.MenuPageRow{
		menuPageTestRow(1, 2, 1),  // 与 2 互为父子（环）
		menuPageTestRow(2, 1, 2),  // 同上
		menuPageTestRow(3, 99, 3), // 父不在集合里
		menuPageTestRow(4, 4, 4),  // 指向自身
		menuPageTestRow(5, 0, 5),
	}
	out := flattenMenuPageRows(buildMenuPageForest(rows, nil))
	if len(out) != 5 {
		t.Fatalf("坏数据不该让行消失：应有 5 行，实际 %d 行", len(out))
	}
	seen := make(map[uint64]bool, len(out))
	for _, r := range out {
		seen[r.ID] = true
	}
	for _, id := range []uint64{1, 2, 3, 4, 5} {
		if !seen[id] {
			t.Errorf("行 %d 应仍在列表里", id)
		}
	}
}

// TestPaginateMenuRootsClampsPage 分页按顶级节点切，页码越界回落最后一页。
func TestPaginateMenuRootsClampsPage(t *testing.T) {
	rows := []adminmodel.MenuPageRow{
		menuPageTestRow(1, 0, 1),
		menuPageTestRow(2, 1, 2),
		menuPageTestRow(3, 0, 3),
		menuPageTestRow(4, 0, 4),
	}
	forest := buildMenuPageForest(rows, nil)
	if got := len(paginateMenuRoots(forest, 1, 2)); got != 2 {
		t.Errorf("第 1 页应有 2 棵顶级树，实际 %d", got)
	}
	// 第 1 页的第二棵子树（id=2）跟着它的根走，不落到下一页。
	page1 := flattenMenuPageRows(paginateMenuRoots(forest, 1, 2))
	if len(page1) != 3 || page1[0].ID != 1 || page1[1].ID != 2 {
		t.Errorf("第 1 页应为「根 1 + 它的子 2 + 根 3」，实际 %d 行", len(page1))
	}
	if got := paginateMenuRoots(forest, 99, 2); len(got) != 1 || got[0].row.ID != 4 {
		t.Errorf("越界页码应回落到最后一页（根 4），实际 %d 棵", len(got))
	}
	if got := paginateMenuRoots(forest, 0, 2); len(got) != 2 {
		t.Errorf("page<1 应按第 1 页处理，实际 %d 棵", len(got))
	}
	if got := paginateMenuRoots(nil, 1, 20); got != nil {
		t.Errorf("空森林应返回 nil，实际 %d 棵", len(got))
	}
}

// TestBuildMenuParentChoicesIndent 上级下拉按父链深度缩进（缺项会让「建子菜单选不到父级」不是问题，
// 但缩进错了会让人把子目录当顶级）。
func TestBuildMenuParentChoicesIndent(t *testing.T) {
	parents := []adminmodel.MenuParentOption{
		{ID: 1, ParentID: 0, Title: "目录"},
		{ID: 2, ParentID: 1, Title: "子目录"},
		{ID: 3, ParentID: 2, Title: "菜单"},
		// 悬空父：链断在集合外，深度按已能走到的层级算，不能死循环。
		{ID: 4, ParentID: 404, Title: "孤儿"},
	}
	out := buildMenuParentChoices(parents, nil)
	if len(out) != 4 {
		t.Fatalf("候选应与输入等长（全量、不分页），实际 %d", len(out))
	}
	wantIndent := map[uint64]string{1: "", 2: "　", 3: "　　", 4: ""}
	for _, c := range out {
		if got := c.Indent; got != wantIndent[c.ID] {
			t.Errorf("候选 %d 的缩进应为 %q，实际 %q", c.ID, wantIndent[c.ID], got)
		}
		if c.Disabled {
			t.Errorf("候选 %d 在无排除集合时不该被标为不可选", c.ID)
		}
	}
}

// TestBuildMenuParentChoicesDisabled 排除集合里的候选标不可选，集合外的不受影响。
//
// 这条是编辑抽屉的核心约束：选自己或自己的子孙必然成环（service 的 menuCheckCircle 会拦），
// UI 不标出来的话用户只会白点一次并收到一个「父子级不能互相设置」的错误。
func TestBuildMenuParentChoicesDisabled(t *testing.T) {
	parents := []adminmodel.MenuParentOption{
		{ID: 1, ParentID: 0, Title: "目录"},
		{ID: 2, ParentID: 1, Title: "子目录"},
		{ID: 3, ParentID: 2, Title: "菜单"},
		{ID: 9, ParentID: 0, Title: "另一个目录"},
	}
	// 编辑 2：它自己（2）与它的子孙（3）不可选，1 和 9 仍可选。
	out := buildMenuParentChoices(parents, map[uint64]bool{2: true, 3: true})
	wantDisabled := map[uint64]bool{1: false, 2: true, 3: true, 9: false}
	for _, c := range out {
		if got := c.Disabled; got != wantDisabled[c.ID] {
			t.Errorf("候选 %d 的不可选态应为 %v，实际 %v", c.ID, wantDisabled[c.ID], got)
		}
	}
}

// TestBuildMenuParentChoicesSurvivesCycle 环数据不能让下拉缩进计算转不出来。
func TestBuildMenuParentChoicesSurvivesCycle(t *testing.T) {
	parents := []adminmodel.MenuParentOption{
		{ID: 1, ParentID: 2, Title: "a"},
		{ID: 2, ParentID: 1, Title: "b"},
	}
	out := buildMenuParentChoices(parents, nil)
	if len(out) != 2 {
		t.Fatalf("环数据下候选仍是两行，实际 %d", len(out))
	}
	for _, c := range out {
		if len(c.Indent) > menuParentMaxDepth*3 {
			t.Errorf("候选 %d 的缩进 %q 超出深度上限，像是不停地在环里加层", c.ID, c.Indent)
		}
	}
}
