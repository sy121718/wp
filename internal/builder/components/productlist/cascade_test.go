package productlist

import "testing"

func treeFixture() map[string][]string {
	return map[string][]string{
		"ali":   {"ingot", "swirl"},
		"ingot": {"ingot-a", "ingot-b"},
		"amigo": {"go"},
	}
}

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

func TestCascadeDeselectParentClearsSubtree(t *testing.T) {
	fixture := treeFixture()

	selected := map[string]bool{}
	for _, id := range cascadeSelection("ali", selected, fixture) {
		selected[id] = true
	}
	got := cascadeSelection("ali", selected, fixture)
	if len(got) != 0 {
		t.Fatalf("取消父级应清空整棵子树，实际还剩 %v", got)
	}
}

func TestCascadeDeselectLeafKeepsSiblings(t *testing.T) {
	fixture := treeFixture()

	full := map[string]bool{}
	for _, id := range cascadeSelection("ali", map[string]bool{}, fixture) {
		full[id] = true
	}
	selected := map[string]bool{}
	for _, id := range cascadeSelection("ingot-a", full, fixture) {
		selected[id] = true
	}

	if selected["ingot-a"] {
		t.Fatalf("ingot-a 应已被取消: %v", selected)
	}

	if subtreeFullySelected("ali", selected, fixture) {
		t.Fatalf("取消一个叶子后父级不应仍是全选: %v", selected)
	}
	if !subtreeAnySelected("ali", selected, fixture) {
		t.Fatalf("父级子树里还有选中的，应当算半选: %v", selected)
	}

	for _, id := range []string{"ali", "ingot", "ingot-b", "swirl"} {
		if !selected[id] {
			t.Fatalf("取消叶子不应影响 %s: %v", id, selected)
		}
	}
}

func TestCascadeLeafSelectDoesNotSelectParent(t *testing.T) {
	got := cascadeSelection("ingot-a", map[string]bool{}, treeFixture())
	if len(got) != 1 || got[0] != "ingot-a" {
		t.Fatalf("勾叶子只应勾它自己，实际 %v", got)
	}
}

func TestCascadeSurvivesCycle(t *testing.T) {
	cyclic := map[string][]string{"a": {"b"}, "b": {"a"}}
	got := cascadeSelection("a", map[string]bool{}, cyclic)

	if len(got) != 2 {
		t.Fatalf("环上应恰好勾到 a 与 b，实际 %v", got)
	}
}
