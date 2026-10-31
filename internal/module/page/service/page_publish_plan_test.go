package pageservice

import "testing"

func TestLangSetDiff(t *testing.T) {
	t.Run("完全相同不报漂移", func(t *testing.T) {
		added, removed := langSetDiff([]string{"zh-CN", "en-US"}, []string{"zh-CN", "en-US"})
		if added != nil || removed != nil {
			t.Fatalf("集合相同不应报漂移：added=%v removed=%v", added, removed)
		}
	})

	t.Run("顺序变化不算漂移（is_default 换人）", func(t *testing.T) {
		added, removed := langSetDiff([]string{"zh-CN", "en-US"}, []string{"en-US", "zh-CN"})
		if added != nil || removed != nil {
			t.Fatalf("仅顺序变化不应报漂移：added=%v removed=%v", added, removed)
		}
	})

	t.Run("空白差异不算漂移", func(t *testing.T) {
		added, removed := langSetDiff([]string{" zh-CN ", "en-US"}, []string{"zh-CN", "en-US"})
		if added != nil || removed != nil {
			t.Fatalf("空白差异不应报漂移：added=%v removed=%v", added, removed)
		}
	})

	t.Run("新增语言只报 added", func(t *testing.T) {
		added, removed := langSetDiff([]string{"zh-CN", "en-US"}, []string{"zh-CN", "en-US", "fr-FR"})
		if len(added) != 1 || added[0] != "fr-FR" || removed != nil {
			t.Fatalf("新增语言应只报 added=[fr-FR]，实际 added=%v removed=%v", added, removed)
		}
	})

	t.Run("移除语言只报 removed", func(t *testing.T) {
		added, removed := langSetDiff([]string{"zh-CN", "en-US"}, []string{"zh-CN"})
		if added != nil || len(removed) != 1 || removed[0] != "en-US" {
			t.Fatalf("移除语言应只报 removed=[en-US]，实际 added=%v removed=%v", added, removed)
		}
	})
}
