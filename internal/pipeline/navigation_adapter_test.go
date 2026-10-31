package pipeline

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

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
		{Label: "123", URL: "/n"},
	}
	return a, items
}

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

func TestTranslateLabelsFallsBackToSource(t *testing.T) {
	a, items := navFixture("en-US", nil)
	a.translateLabels(items, "")
	if items[0].Label != "首页" || items[1].Children[0].Label != "新品" {
		t.Fatalf("无译文应回退原文，实际 %q / %q", items[0].Label, items[1].Children[0].Label)
	}
}

func TestTranslateLabelsSkipsWhenNoLang(t *testing.T) {
	a, items := navFixture("", map[string]string{
		i18n.ContentHash("首页") + "|": "Home",
	})
	a.translateLabels(items, "")
	if items[0].Label != "首页" {
		t.Fatalf("未指定语言时不应翻译，实际 %q", items[0].Label)
	}
}

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
