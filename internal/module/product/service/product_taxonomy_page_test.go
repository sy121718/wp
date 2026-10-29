package productservice

// product_taxonomy_page_test.go — 分类 / 品牌 / 标签三页读模型的纯逻辑判据。
//
// 三页的取数顺序（工程 → 计数 → 收敛 → 取页）与失败档位在 feature 测试里用真实库覆盖
//（public/test/product/feature/ 的分类页 / 品牌页 / 标签页用例）；这里钉的是**没有库也能
// 成立**的那几条，它们恰好是最容易被后来的重构悄悄改掉的：候选集合的判据、缺失父级的挑选、
// 逐个父级的读取行为。

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// TestFlattenCategoryTreeKeepsParentBeforeChildren DFS 扁平：父在前、子紧随（模板按 Depth 缩进）。
//
// 反面判据：按 ID 排序或按层分组时，子行会跑到父行前面 —— 缩进还在，但「这行属于谁」
// 读起来全错，而页面照常 200。
func TestFlattenCategoryTreeKeepsParentBeforeChildren(t *testing.T) {
	tree := []*productdto.CategoryResp{
		{ID: "b", Name: "男装", Children: []*productdto.CategoryResp{
			{ID: "b1", Name: "衬衫", Children: []*productdto.CategoryResp{{ID: "b1a", Name: "短袖"}}},
			{ID: "b2", Name: "裤装"},
		}},
		{ID: "a", Name: "女装"},
	}
	flat := FlattenCategoryTree(tree)
	want := []string{"b", "b1", "b1a", "b2", "a"}
	if len(flat) != len(want) {
		t.Fatalf("应扁平出 %d 行，实际 %d", len(want), len(flat))
	}
	for i, id := range want {
		if flat[i].ID != id {
			t.Fatalf("第 %d 行应为 %s，实际 %s（顺序：%v）", i, id, flat[i].ID, idsOfCategories(flat))
		}
	}
}

func idsOfCategories(list []*productdto.CategoryResp) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, n.ID)
	}
	return out
}

// TestCategoriesInProjectDropsForeignRows 候选只收本工程分类：判据是行自己的 ProjectID，
// **不依赖 RLS**（策略未切非超级角色时 ListCategories 会把别的工程的分类一起返回）。
//
// 混进来会让「父级在别的工程」这种坏数据在界面上看起来完全正常 —— 而它恰恰是
// 列表行徽章与抽屉里那句「不属于本工程」要说明的情形。
func TestCategoriesInProjectDropsForeignRows(t *testing.T) {
	flat := []*productdto.CategoryResp{
		{ID: "a", ProjectID: "p1", Name: "男装"},
		{ID: "b", ProjectID: "p2", Name: "别的工程的分类"},
		{ID: "c", ProjectID: "p1", Name: "女装"},
	}
	got := CategoriesInProject(flat, "p1")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("应只保留本工程分类 [a c]，实际 %v", idsOfCategories(got))
	}
	if got := CategoriesInProject(flat, ""); len(got) != 0 {
		t.Fatalf("工程为空时不该给出任何候选，实际 %d 条", len(got))
	}
}
