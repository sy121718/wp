package adminmodel_test

import (
	"context"
	"testing"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/public/test/support"
)

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

	for id, want := range map[uint64]bool{root: true, child: true, grand: false, root2: false} {
		if got := byID[id].HasChildren; got != want {
			t.Errorf("行 %d 的 HasChildren 应为 %v，实际 %v", id, want, got)
		}
	}

	if got := byID[root].PermissionCodes; len(got) != 1 || got[0] != "dir:read" {
		t.Errorf("目录甲的权限码应为 [dir:read]，实际 %v", got)
	}
	if got := byID[grand].PermissionCodes; len(got) != 0 {
		t.Errorf("未绑权限码的行应为空集合，实际 %v", got)
	}

	for _, r := range rows {
		if r.Matched {
			t.Errorf("浏览态行 %d 不该带 Matched", r.ID)
		}
	}
}

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

	clamped, _, err := m.ListMenuRootsPage(ctx, 9, 1)
	if err != nil {
		t.Fatalf("ListMenuRootsPage(page=9): %v", err)
	}
	if len(clamped) != 1 || clamped[0].ID != root3 {
		t.Fatalf("越界页码应回落到最后一页（目录三），实际 %d 行", len(clamped))
	}

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

func TestMenuSearchForestBringsAncestorsOnly(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := adminmodel.NewMenuModel(db)
	clearSeededMenus(t, m)
	ctx := context.Background()

	root := newMenu(t, m, "目录甲", 0, 1, "")
	dir := newMenu(t, m, "子目录", root, 2, "")
	leaf := newMenu(t, m, "内容模板", dir, 3, "/admin/content-templates", "contenttemplate:list")

	button := newMenu(t, m, "内容模板删除", leaf, 4, "", "contenttemplate:delete")

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

	for id, want := range map[uint64]bool{root: false, dir: false, leaf: true} {
		if got := byID[id].Matched; got != want {
			t.Errorf("行 %d 的 Matched 应为 %v，实际 %v", id, want, got)
		}
	}

	for id, want := range map[uint64]bool{root: true, dir: true, leaf: false} {
		if got := byID[id].HasChildren; got != want {
			t.Errorf("行 %d 的 HasChildren 应为 %v（按本次读出的集合算），实际 %v", id, want, got)
		}
	}

	rows, err = m.ListMenuSearchForest(ctx, "content-templates")
	if err != nil {
		t.Fatalf("ListMenuSearchForest(路径): %v", err)
	}
	if !menuRowsByID(rows)[leaf].Matched {
		t.Fatal("按路径片段应命中内容模板一行")
	}

	rows, err = m.ListMenuSearchForest(ctx, "不存在的关键词-9f3c")
	if err != nil {
		t.Fatalf("ListMenuSearchForest(无命中): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("无命中时应返回空集，实际 %d 行", len(rows))
	}
}

func matchedIDs(rows []adminmodel.MenuPageRow) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if r.Matched {
			out = append(out, r.ID)
		}
	}
	return out
}

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

	leaf, err := m.ListSubtreeIDs(ctx, grand)
	if err != nil {
		t.Fatalf("ListSubtreeIDs(叶): %v", err)
	}
	if len(leaf) != 1 || !leaf[grand] {
		t.Errorf("叶节点的子树集合应只有它自己，实际 %v", leaf)
	}

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
