package adminmodel_test

// menu_tree_model_test.go — 菜单树状分页的真库行为验证。
//
// 为什么必须有这一条：分页单位从「行」换成「顶级节点」之后，正确性全落在几条
// **不报错、只是结果不同**的语义上：
//
//   - 子树完整（少一层不报错，只是树上少一截，用户看到半棵树）；
//   - 孤儿算根（父级被软删的子菜单既不是根、也不在别人的子树里，漏掉它等于「整行消失」）；
//   - 分页切分不切断子树（limit=1 时一页仍是「一棵完整的树」而不是「一行」）；
//   - 搜索森林的方向（向上取祖先，不是向下取子孙：命中项子树**不**带出来）；
//   - matched 标记落点（只有命中项，祖先只是陪衬）。
//
// 表结构来自**生产迁移**（support.NewMigratedPGTestDB 复制的模板库），不手抄 CREATE TABLE。
// 数据一律走生产写入路径（CreateWithPermissionCodes / SoftDeleteWithPermissionCodes），
// 这样权限码关联表也顺带被种上，不必另写一份 seed SQL。

import (
	"context"
	"testing"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/public/test/support"
)

// newMenu 走生产写入路径插一个菜单，返回它的 id。
func newMenu(t *testing.T, m *adminmodel.MenuModel, title string, parentID uint64, sort int, path string, codes ...string) uint64 {
	t.Helper()
	e := &adminmodel.MenuEntity{
		Title: title, ParentID: parentID, Type: adminmodel.MenuTypeMenu,
		Path: path, Status: adminmodel.MenuStatusEnabled, SortOrder: sort,
	}
	if err := m.CreateWithPermissionCodes(context.Background(), e, codes); err != nil {
		t.Fatalf("插入菜单 %s 失败: %v", title, err)
	}
	return e.ID
}

// clearSeededMenus 把模板库里的种子菜单软删掉。
//
// 模板库是「迁移 + 种子」跑完的库，菜单表里已有十几条生产菜单（侧栏那批），
// 而「分页单位 = 顶级节点数」这类断言必须只面对本用例自己造的数据，否则数字没有意义。
// 用生产写入路径软删而不是 DELETE：菜单 id 被角色 / 授权表引用，物理删除会撞外键。
func clearSeededMenus(t *testing.T, m *adminmodel.MenuModel) {
	t.Helper()
	ctx := context.Background()
	all, err := m.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) == 0 {
		return
	}
	ids := make([]uint64, 0, len(all))
	for _, e := range all {
		ids = append(ids, e.ID)
	}
	if _, err := m.SoftDeleteWithPermissionCodes(ctx, ids); err != nil {
		t.Fatalf("清理种子菜单: %v", err)
	}
}

func menuRowsByID(rows []adminmodel.MenuPageRow) map[uint64]adminmodel.MenuPageRow {
	out := make(map[uint64]adminmodel.MenuPageRow, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// TestMenuRootsPageCarriesWholeSubtree 一页 = 顶级节点 + 它的完整子树，分页单位是顶级节点。
func TestMenuRootsPageCarriesWholeSubtree(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := adminmodel.NewMenuModel(db)
	clearSeededMenus(t, m)
	ctx := context.Background()

	root := newMenu(t, m, "目录甲", 0, 1, "", "dir:read")
	child := newMenu(t, m, "菜单甲", root, 2, "/admin/a")
	grand := newMenu(t, m, "按钮甲", child, 3, "")
	root2 := newMenu(t, m, "目录乙", 0, 9, "")

	rows, total, err := m.ListMenuRootsPage(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListMenuRootsPage: %v", err)
	}
	if total != 2 {
		t.Fatalf("分页单位是顶级节点，应为 2，实际 %d", total)
	}
	if len(rows) != 4 {
		t.Fatalf("一页应带回两棵树的全部 4 行，实际 %d 行", len(rows))
	}
	byID := menuRowsByID(rows)
	for _, id := range []uint64{root, child, grand, root2} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("行 %d 应在本页里", id)
		}
	}
	// has_children 按**本批集合**算：孙节点没有子行，不该渲染折叠三角。
	for id, want := range map[uint64]bool{root: true, child: true, grand: false, root2: false} {
		if got := byID[id].HasChildren; got != want {
			t.Errorf("行 %d 的 HasChildren 应为 %v，实际 %v", id, want, got)
		}
	}
	// 权限码随行回填（列表要显示「这个节点代表哪些权限」）。
	if got := byID[root].PermissionCodes; len(got) != 1 || got[0] != "dir:read" {
		t.Errorf("目录甲的权限码应为 [dir:read]，实际 %v", got)
	}
	if got := byID[grand].PermissionCodes; len(got) != 0 {
		t.Errorf("未绑权限码的行应为空集合，实际 %v", got)
	}
	// 浏览态没有命中标记。
	for _, r := range rows {
		if r.Matched {
			t.Errorf("浏览态行 %d 不该带 Matched", r.ID)
		}
	}
}

// TestMenuRootsPageKeepsSubtreeIntactAndClampsPage 分页按顶级节点切，越界回最后一页。
func TestMenuRootsPageKeepsSubtreeIntactAndClampsPage(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := adminmodel.NewMenuModel(db)
	clearSeededMenus(t, m)
	ctx := context.Background()

	root1 := newMenu(t, m, "目录一", 0, 1, "")
	newMenu(t, m, "菜单一", root1, 2, "/admin/one")
	newMenu(t, m, "按钮一", root1, 3, "")
	root2 := newMenu(t, m, "目录二", 0, 2, "")
	root3 := newMenu(t, m, "目录三", 0, 3, "")

	page1, total, err := m.ListMenuRootsPage(ctx, 1, 1)
	if err != nil {
		t.Fatalf("ListMenuRootsPage(page=1): %v", err)
	}
	if total != 3 {
		t.Fatalf("应有 3 个顶级节点，实际 %d", total)
	}
	if len(page1) != 3 {
		t.Fatalf("limit=1 时一页仍是**一棵完整的树**（3 行），实际 %d 行", len(page1))
	}
	if page1[0].ID != root1 {
		t.Fatalf("第一页应以 sort_order 最小的顶级节点开头，实际 %d", page1[0].ID)
	}

	page2, _, err := m.ListMenuRootsPage(ctx, 2, 1)
	if err != nil {
		t.Fatalf("ListMenuRootsPage(page=2): %v", err)
	}
	if len(page2) != 1 || page2[0].ID != root2 {
		t.Fatalf("第二页应为「目录二」一行，实际 %d 行", len(page2))
	}

	// 越界：删到只剩一页时，用户手上的 ?page=9 必须回到最后一页，而不是一张空表
	//（空表看起来像「数据全没了」）。
	clamped, _, err := m.ListMenuRootsPage(ctx, 9, 1)
	if err != nil {
		t.Fatalf("ListMenuRootsPage(page=9): %v", err)
	}
	if len(clamped) != 1 || clamped[0].ID != root3 {
		t.Fatalf("越界页码应回落到最后一页（目录三），实际 %d 行", len(clamped))
	}

	// 根节点被软删：它不再是分页单位，它的子行仍跟着走（这里 root2 无子，直接消失）。
	if _, err := m.SoftDeleteWithPermissionCodes(ctx, []uint64{root2}); err != nil {
		t.Fatalf("软删目录二: %v", err)
	}
	_, total2, err := m.ListMenuRootsPage(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListMenuRootsPage(软删后): %v", err)
	}
	if total2 != 2 {
		t.Fatalf("软删一个顶级节点后应为 2 个，实际 %d", total2)
	}
}

// TestMenuRootsPageTreatsOrphanAsRoot 父级已不在（软删）的子菜单必须仍然出现，且算作根。
//
// 这是树状分页**新增**的责任：旧的行分页照常把这一行读出来（只是上级列为空），
// 而逐层展开的树会让它既不是根、也不在任何可见父节点的子树里 —— 整行消失。
func TestMenuRootsPageTreatsOrphanAsRoot(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := adminmodel.NewMenuModel(db)
	clearSeededMenus(t, m)
	ctx := context.Background()

	parent := newMenu(t, m, "会被删掉的目录", 0, 1, "")
	orphan := newMenu(t, m, "留下来的菜单", parent, 2, "/admin/kept")

	if _, err := m.SoftDeleteWithPermissionCodes(ctx, []uint64{parent}); err != nil {
		t.Fatalf("软删父级: %v", err)
	}
	rows, total, err := m.ListMenuRootsPage(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListMenuRootsPage: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != orphan {
		t.Fatalf("父级已软删的子菜单应作为一个根出现（恰 1 行 %d），实际 total=%d rows=%d",
			orphan, total, len(rows))
	}
}

// TestMenuSearchForestBringsAncestorsOnly 搜索结果 = 命中项 + 各自到根的祖先，**不带**命中项子树。
func TestMenuSearchForestBringsAncestorsOnly(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := adminmodel.NewMenuModel(db)
	clearSeededMenus(t, m)
	ctx := context.Background()

	root := newMenu(t, m, "目录甲", 0, 1, "")
	dir := newMenu(t, m, "子目录", root, 2, "")
	leaf := newMenu(t, m, "内容模板", dir, 3, "/admin/content-templates", "contenttemplate:list")
	// 命中项的子树：搜索不该把它带出来（带出来会让页面上多出一堆并不匹配的行，
	// 也会让同一棵树在分页里成倍膨胀）。
	button := newMenu(t, m, "内容模板删除", leaf, 4, "", "contenttemplate:delete")
	// 无关分支：既不是命中项也不是祖先，必须不出现。
	other := newMenu(t, m, "无关目录", 0, 9, "")

	rows, err := m.ListMenuSearchForest(ctx, "contenttemplate:list")
	if err != nil {
		t.Fatalf("ListMenuSearchForest(权限码): %v", err)
	}
	if got := matchedIDs(rows); len(got) != 1 || got[0] != leaf {
		t.Fatalf("按权限码只应命中内容模板一行，实际命中 %v", got)
	}
	byID := menuRowsByID(rows)
	for _, id := range []uint64{root, dir, leaf} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("祖先/命中行 %d 应被带出，实际行集 %v", id, byID)
		}
	}
	for _, id := range []uint64{button, other} {
		if _, ok := byID[id]; ok {
			t.Errorf("行 %d 不该出现在搜索结果里（命中项子树 / 无关分支）", id)
		}
	}
	// matched 只落在命中项上，祖先只是「仅供定位」的路径。
	for id, want := range map[uint64]bool{root: false, dir: false, leaf: true} {
		if got := byID[id].Matched; got != want {
			t.Errorf("行 %d 的 Matched 应为 %v，实际 %v", id, want, got)
		}
	}
	// has_children 只看**本集合**：命中行在本集合内没有子行 → 不该渲染折叠三角。
	for id, want := range map[uint64]bool{root: true, dir: true, leaf: false} {
		if got := byID[id].HasChildren; got != want {
			t.Errorf("行 %d 的 HasChildren 应为 %v（按本次读出的集合算），实际 %v", id, want, got)
		}
	}

	// 标题 / 路径也参与匹配：搜路径片段应命中同一行。
	rows, err = m.ListMenuSearchForest(ctx, "content-templates")
	if err != nil {
		t.Fatalf("ListMenuSearchForest(路径): %v", err)
	}
	if !menuRowsByID(rows)[leaf].Matched {
		t.Fatal("按路径片段应命中内容模板一行")
	}

	// 没有命中时返回空集（不是全表）。
	rows, err = m.ListMenuSearchForest(ctx, "不存在的关键词-9f3c")
	if err != nil {
		t.Fatalf("ListMenuSearchForest(无命中): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("无命中时应返回空集，实际 %d 行", len(rows))
	}
}

// matchedIDs 取命中项的 id 列表（断言命中范围用）。
func matchedIDs(rows []adminmodel.MenuPageRow) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if r.Matched {
			out = append(out, r.ID)
		}
	}
	return out
}

// TestListSubtreeIDs 子树集合要含根自身（它同样不能当自己的上级），且不越界到兄弟分支。
//
// 这条支撑的是编辑抽屉的「上级菜单」下拉：集合里的是灰掉的候选。
// 少了根自身 → 用户能把菜单挂到自己名下（service 报错但按钮可点）；
// 多了兄弟分支 → 明明合法的选择被灰掉，用户没法把菜单挪过去。
func TestListSubtreeIDs(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := adminmodel.NewMenuModel(db)
	clearSeededMenus(t, m)
	ctx := context.Background()

	root := newMenu(t, m, "目录甲", 0, 1, "")
	child := newMenu(t, m, "菜单甲", root, 2, "/admin/a")
	grand := newMenu(t, m, "按钮甲", child, 3, "")
	sibling := newMenu(t, m, "目录乙", 0, 4, "")

	ids, err := m.ListSubtreeIDs(ctx, root)
	if err != nil {
		t.Fatalf("ListSubtreeIDs: %v", err)
	}
	for _, id := range []uint64{root, child, grand} {
		if !ids[id] {
			t.Errorf("菜单 %d 应在 %d 的子树集合里", id, root)
		}
	}
	if ids[sibling] {
		t.Errorf("兄弟分支 %d 不该在 %d 的子树集合里", sibling, root)
	}
	if len(ids) != 3 {
		t.Errorf("子树集合应恰好是 3 个节点，实际 %d", len(ids))
	}

	// 叶子：集合就是它自己 —— 「把菜单挂到自己名下」也要被挡住。
	leaf, err := m.ListSubtreeIDs(ctx, grand)
	if err != nil {
		t.Fatalf("ListSubtreeIDs(叶): %v", err)
	}
	if len(leaf) != 1 || !leaf[grand] {
		t.Errorf("叶节点的子树集合应只有它自己，实际 %v", leaf)
	}

	// 新建态（0）与已被删掉的 id：空集合，不是错误 —— 调用方拿到的是「没有不可选项」。
	for _, id := range []uint64{0, 99999999} {
		empty, err := m.ListSubtreeIDs(ctx, id)
		if err != nil {
			t.Fatalf("ListSubtreeIDs(%d): %v", id, err)
		}
		if len(empty) != 0 {
			t.Errorf("ListSubtreeIDs(%d) 应为空集合，实际 %v", id, empty)
		}
	}
}
