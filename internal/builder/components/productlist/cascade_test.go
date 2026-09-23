// cascade_test.go — 分类树的级联选中语义（勾父带子、取消父清子、子级影响父级三态）。
//
// 这三条是树形多选的**用户预期**，错了不会报任何错：
//
//	· 勾父不带子 ⇒ 选 Alibarbar 只筛到直接挂在它名下的商品（用户以为是整个品牌）；
//	· 取消父不清子 ⇒ 子级还勾着，父级看起来像半选，用户以为取消失败；
//	· 父级态不看子级 ⇒ 手动取消一个子级后父级还显示全选，与实际筛选结果不符。
package productlist

import "testing"

// treeFixture 两棵树：Ali(Ingot, Swirl) 与 Amigo(Go)，外加叶子 KUZ。
func treeFixture() map[string][]string {
	return map[string][]string{
		"ali":   {"ingot", "swirl"},
		"ingot": {"ingot-a", "ingot-b"},
		"amigo": {"go"},
	}
}

// TestCascadeSelectParentSelectsSubtree 勾父级 ⇒ 整棵子树（含孙级）都进集合。
func TestCascadeSelectParentSelectsSubtree(t *testing.T) {
	got := cascadeSelection("ali", map[string]bool{}, treeFixture())
	want := []string{"ali", "ingot", "ingot-a", "ingot-b", "swirl"}
	if len(got) != len(want) {
		t.Fatalf("勾父级应连子级与孙级一起勾：期望 %v，实际 %v", want, got)
	}
	set := map[string]bool{}
	for _, id := range got {
		set[id] = true
	}
	for _, id := range want {
		if !set[id] {
			t.Fatalf("勾父级漏了 %s：%v", id, got)
		}
	}
}

// TestCascadeDeselectParentClearsSubtree 取消父级 ⇒ 整棵子树都清掉（干净的取消）。
func TestCascadeDeselectParentClearsSubtree(t *testing.T) {
	fixture := treeFixture()
	// 先全选父级，再取消它。
	selected := map[string]bool{}
	for _, id := range cascadeSelection("ali", selected, fixture) {
		selected[id] = true
	}
	got := cascadeSelection("ali", selected, fixture)
	if len(got) != 0 {
		t.Fatalf("取消父级应清空整棵子树，实际还剩 %v", got)
	}
}

// TestCascadeDeselectLeafKeepsSiblings 取消一个叶子 ⇒ 兄弟仍在，父级成为半选。
func TestCascadeDeselectLeafKeepsSiblings(t *testing.T) {
	fixture := treeFixture()
	// cascadeSelection 返回的是**完整的新集合**（不是增量），所以每次都用它整体替换。
	full := map[string]bool{}
	for _, id := range cascadeSelection("ali", map[string]bool{}, fixture) {
		full[id] = true
	}
	selected := map[string]bool{}
	for _, id := range cascadeSelection("ingot-a", full, fixture) {
		selected[id] = true
	}
	// 只有 ingot-a 被取消。
	if selected["ingot-a"] {
		t.Fatalf("ingot-a 应已被取消: %v", selected)
	}
	// 父级不再是「全选」，但子树里还有选中的 ⇒ 渲染期会算成半选。
	if subtreeFullySelected("ali", selected, fixture) {
		t.Fatalf("取消一个叶子后父级不应仍是全选: %v", selected)
	}
	if !subtreeAnySelected("ali", selected, fixture) {
		t.Fatalf("父级子树里还有选中的，应当算半选: %v", selected)
	}
	// 兄弟与父级本身都还在集合里。
	for _, id := range []string{"ali", "ingot", "ingot-b", "swirl"} {
		if !selected[id] {
			t.Fatalf("取消叶子不应影响 %s: %v", id, selected)
		}
	}
}

// TestCascadeLeafSelectDoesNotSelectParent 勾叶子不自动勾父级（父级态由子级推导）。
func TestCascadeLeafSelectDoesNotSelectParent(t *testing.T) {
	got := cascadeSelection("ingot-a", map[string]bool{}, treeFixture())
	if len(got) != 1 || got[0] != "ingot-a" {
		t.Fatalf("勾叶子只应勾它自己，实际 %v", got)
	}
}

// TestCascadeSurvivesCycle 环形数据不会无限递归（兜底而不是崩溃）。
func TestCascadeSurvivesCycle(t *testing.T) {
	cyclic := map[string][]string{"a": {"b"}, "b": {"a"}}
	got := cascadeSelection("a", map[string]bool{}, cyclic)
	// a 与 b 都该被勾上，且函数必须返回（不挂起）。
	if len(got) != 2 {
		t.Fatalf("环上应恰好勾到 a 与 b，实际 %v", got)
	}
}
