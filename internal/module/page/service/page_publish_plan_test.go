package pageservice

// page_publish_plan_test.go — 发布计划漂移判定的纯逻辑（审计 I18N-01 续第 3 条）。
//
// 「冻结计划的语言集合 ≠ 当前站点语言集合」要在构建/发布时**可见**（结构化日志）。
// 判定本身是纯集合比较，单独钉住两条容易写错的性质：
//   - **顺序无关**：默认语言在前是实现细节，is_default 换人（清单重排）不是漂移；
//   - **空白无关**：语言码两侧空白不该造成一条假的「配置不一致」。
//
// 记日志那一段不做断言（logger 未初始化时是空操作），断言的是它据以判定的 diff。

import "testing"

// TestLangSetDiff 集合语义的差异判定。
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
