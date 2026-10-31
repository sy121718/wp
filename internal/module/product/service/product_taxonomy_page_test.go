package productservice

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

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
