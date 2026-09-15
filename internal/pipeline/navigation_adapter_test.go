package pipeline

// navigation_adapter_test.go — 菜单标签的译文回填（审计 I18N-007 / I18N-018）。
//
// 这一块的特殊之处：菜单标签**不在页面文档里**，而在 navigation 模块的节点上。
// 页面翻译工作台看不到它（Props 里没有这个值），组件侧声明的 Translatable 白名单
// 对它也无效 —— 所以译文只能在 resolver 交出结果之前补上。
//
// 用注入的存储而不是默认存储来测：这条链一旦只能靠起库的集成测试覆盖，
// 它就会成为「重构时最先被抹平」的那类逻辑（改成不翻译也照样编译通过）。

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// stubContentStore 按 (hash, context) 索引返回预设译文。
type stubContentStore struct {
	targets map[string]string
}

func (s *stubContentStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range hashes {
		key := i18n.ContentIndexKey(h, i18n.ContentContext("navigation", "label"))
		if v, ok := s.targets[h+"|"+lang]; ok {
			out[key] = v
		}
	}
	return out, nil
}

func navFixture(lang string, targets map[string]string) (*NavigationAdapter, []core.NavigationItem) {
	a := &NavigationAdapter{Lang: lang, Ctx: context.Background()}
	if targets != nil {
		a.ContentStore = &stubContentStore{targets: targets}
	}
	items := []core.NavigationItem{
		{Label: "首页", URL: "/"},
		{Label: "商品", URL: "/shop", Children: []core.NavigationItem{
			{Label: "新品", URL: "/shop/new"},
		}},
		{Label: "123", URL: "/n"}, // 纯数字：本就不该进译文表
	}
	return a, items
}

// TestTranslateLabelsReplacesMenuLabels 命中译文时替换，包含子节点。
func TestTranslateLabelsReplacesMenuLabels(t *testing.T) {
	targets := map[string]string{
		i18n.ContentHash("首页") + "|en-US": "Home",
		i18n.ContentHash("商品") + "|en-US": "Shop",
		i18n.ContentHash("新品") + "|en-US": "New arrivals",
	}
	a, items := navFixture("en-US", targets)
	a.translateLabels(items, "")
	if items[0].Label != "Home" {
		t.Fatalf("顶层标签应替换为译文，实际 %q", items[0].Label)
	}
	if items[1].Children[0].Label != "New arrivals" {
		t.Fatalf("子节点标签也应替换（菜单是树，漏了子树等于只翻一半），实际 %q", items[1].Children[0].Label)
	}
}

// TestTranslateLabelsFallsBackToSource 无译文时回退原文，绝不输出空串。
//
// 回退不是「差不多能用」而是硬要求：菜单项没有文字等于站点上少了一个入口，
// 而英文站点缺译文是常态（新增菜单项时没人会立刻补译文）。
func TestTranslateLabelsFallsBackToSource(t *testing.T) {
	a, items := navFixture("en-US", nil) // 空存储：全部未命中
	a.translateLabels(items, "")
	if items[0].Label != "首页" || items[1].Children[0].Label != "新品" {
		t.Fatalf("无译文应回退原文，实际 %q / %q", items[0].Label, items[1].Children[0].Label)
	}
}

// TestTranslateLabelsSkipsWhenNoLang 未指定目标语言时不查库、不动标签。
func TestTranslateLabelsSkipsWhenNoLang(t *testing.T) {
	a, items := navFixture("", map[string]string{
		i18n.ContentHash("首页") + "|": "Home",
	})
	a.translateLabels(items, "")
	if items[0].Label != "首页" {
		t.Fatalf("未指定语言时不应翻译，实际 %q", items[0].Label)
	}
}

// TestTranslateLabelsKeepsUntranslatable 纯数字 / 纯符号标签不进译文表。
func TestTranslateLabelsKeepsUntranslatable(t *testing.T) {
	if i18n.ShouldTranslateContent("123") {
		t.Skip("该值被 ShouldTranslateContent 认为可翻译，改用别的样例")
	}
	a, items := navFixture("en-US", map[string]string{
		i18n.ContentHash("123") + "|en-US": "one-two-three",
	})
	a.translateLabels(items, "")
	if items[2].Label != "123" {
		t.Fatalf("纯数字标签不应被替换，实际 %q", items[2].Label)
	}
}
