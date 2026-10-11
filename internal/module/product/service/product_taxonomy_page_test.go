package productservice

// product_taxonomy_page_test.go — 分类 / 品牌 / 标签三页读模型的纯逻辑判据。
//
// 三页的取数顺序（工程 → 计数 → 收敛 → 取页）与失败档位在 feature 测试里用真实库覆盖
//（public/test/product/feature/ 的分类页 / 品牌页 / 标签页用例）；这里钉的是**没有库也能
// 成立**的那几条，它们恰好是最容易被后来的重构悄悄改掉的：候选集合的判据、缺失父级的挑选、
// 逐个父级的读取行为。

import (
	"errors"
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

// TestCategoryLabelIndentsByDepth 层级缩进用全角空格（<option> 与列表里都成立）。
func TestCategoryLabelIndentsByDepth(t *testing.T) {
	if got := CategoryLabel(&productdto.CategoryResp{Name: "男装", Depth: 0}); got != "男装" {
		t.Fatalf("顶级分类不带缩进，实际 %q", got)
	}
	if got := CategoryLabel(&productdto.CategoryResp{Name: "衬衫", Depth: 2}); got != "　　衬衫" {
		t.Fatalf("二级分类应缩进两个全角空格，实际 %q", got)
	}
	if got := CategoryLabel(nil); got != "" {
		t.Fatalf("nil 节点应给空串（不让悬空引用把页面打崩），实际 %q", got)
	}
}

// TestMissingParentIDs 只挑「父级不在候选集合里」的 id：去重、保持顺序、跳过空父级。
func TestMissingParentIDs(t *testing.T) {
	nodes := []*productdto.CategoryResp{
		{ID: "a", ParentID: "p1"},
		{ID: "b", ParentID: "p1"}, // 与上一行同一个父级 → 去重
		{ID: "c", ParentID: "p2"},
		{ID: "d"},                      // 顶级分类：没有父级
		{ID: "e", ParentID: "pr1cat1"}, // 父级在候选集合里 → 不算缺失
	}
	known := map[string]struct{}{"pr1cat1": {}}
	got := MissingParentIDs(nodes, known)
	if len(got) != 2 || got[0] != "p1" || got[1] != "p2" {
		t.Fatalf("缺失父级应去重且保序为 [p1 p2]，实际 %v", got)
	}
	if ids := MissingParentIDs(nodes, nil); len(ids) != 3 || ids[0] != "p1" || ids[1] != "p2" || ids[2] != "pr1cat1" {
		t.Fatalf("候选为空时所有非空父级都算缺失（去重后 3 个）：%v", ids)
	}
	if ids := MissingParentIDs(nil, known); len(ids) != 0 {
		t.Fatalf("没有行时不该给出缺失父级，实际 %v", ids)
	}
}

// TestCategoryParentFactsReadsOnceAndSkipsUnreadable 名称来自**读**（不是从 ID 编），
// 同一个父级只读一次；读不到时不写键 —— 模板于是退回裸 ID（有意降级）。
func TestCategoryParentFactsReadsOnceAndSkipsUnreadable(t *testing.T) {
	reads := map[string]int{}
	load := func(id string) (*productdto.CategoryResp, error) {
		reads[id]++
		switch id {
		case "in-project":
			return &productdto.CategoryResp{ID: id, ProjectID: "p1", Name: "男装"}, nil
		case "foreign":
			return &productdto.CategoryResp{ID: id, ProjectID: "prj-other", Name: "女装"}, nil
		default:
			return nil, errors.New("not found")
		}
	}
	got := CategoryParentFacts([]string{"in-project", "foreign", "missing"}, "p1", load)
	if got["in-project"].Name != "男装" || got["in-project"].OutOfScope {
		t.Fatalf("本工程父级应给名称且不标跨工程，实际 %+v", got["in-project"])
	}
	if got["foreign"].Name != "女装" || !got["foreign"].OutOfScope {
		t.Fatalf("跨工程父级应给名称并置 OutOfScope（说明那句由模板取词），实际 %+v", got["foreign"])
	}
	if _, ok := got["missing"]; ok {
		t.Fatalf("读不到时不该给出名称（宁可显示裸 ID，也不显示编造的父级名）：%+v", got)
	}
	for id, n := range reads {
		if n != 1 {
			t.Fatalf("父级 %s 只该读一次，实际 %d 次", id, n)
		}
	}
	if got := CategoryParentFacts(nil, "p1", load); got != nil {
		t.Fatalf("没有缺失父级时给 nil（调用方据此跳过装配），实际 %v", got)
	}
}

// TestClampPageToTotalIsSharedBySubLists 三页共用同一份收敛（属性页与列表页也走它）：
// total=0 收敛到第 1 页，越界收敛到最后一页，非法入参不 panic。
func TestClampPageToTotalIsSharedBySubLists(t *testing.T) {
	cases := []struct {
		page, size int
		total      int64
		want       int
	}{
		{1, 20, 0, 1},
		{3, 20, 25, 2},   // 25 条 = 2 页 → 越界收敛到 2
		{0, 20, 100, 1},  // 页码 0 归一到 1
		{-5, 20, 100, 1}, // 负数同理
		{5, 0, 100, 5},   // size 非法时用默认每页条数：100/20 = 5 页，第 5 页有效
		{9, 0, 100, 5},   // 越界
	}
	for _, c := range cases {
		if got := ClampPageToTotal(c.page, c.size, c.total); got != c.want {
			t.Fatalf("ClampPageToTotal(%d,%d,%d) = %d，want %d", c.page, c.size, c.total, got, c.want)
		}
	}
}

// TestProductSubListPageSizeIsTwenty 每页 20 条是三页共用的口径（控制器不再各写一个字面量）。
func TestProductSubListPageSizeIsTwenty(t *testing.T) {
	if ProductSubListPageSize != 20 {
		t.Fatalf("子列表每页条数应为 20，实际 %d", ProductSubListPageSize)
	}
}
