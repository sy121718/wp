package articlelist

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func alCSSFor(p *Props) string {
	var b core.CSSBuckets
	CompileCSS("t", p, &b)
	return b.String()
}

func alNodeOf(t *testing.T, p *Props) *core.Node {
	t.Helper()
	n := &core.Node{ID: "al1", Type: Type}
	if p != nil {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		n.Props = raw
	}
	return n
}

func TestArticleListCSSContract(t *testing.T) {
	out := alCSSFor(&Props{})
	for _, want := range []string{
		".sky-c-t {",

		"grid-template-columns: var(--sky-al-cols, repeat(3, minmax(0, 1fr)))",

		"@media (max-width: 767px)",
		"grid-template-columns: 1fr",

		"background: var(--sky-c-surface, #fff)",
		"border: 1px solid var(--sky-c-border, #e5e7eb)",
		"color: var(--sky-c-text, #111827)",
		"color: var(--sky-c-text-mute, #6b7280)",
		"color: var(--sky-c-primary, #2563eb)",
		"background: var(--sky-c-bg-soft, #f4f5f6)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "&") {
		t.Errorf("产物里有未展开的占位或未替换的作用域前缀:\n%s", out)
	}
}

func TestArticleListCSSListVariant(t *testing.T) {
	list := alCSSFor(&Props{Layout: LayoutList})
	if !strings.Contains(list, "flex-direction: row") {
		t.Errorf("列表布局缺少左图右文规则:\n%s", list)
	}
	if !strings.Contains(list, "flex: 0 0 min(100%, 220px)") {
		t.Errorf("列表布局图片列应定宽并按视口封顶:\n%s", list)
	}
	grid := alCSSFor(&Props{Layout: LayoutGrid})
	if strings.Contains(grid, "flex-direction: row") {
		t.Errorf("网格布局不该输出列表段:\n%s", grid)
	}
}

func TestArticleListCSSExcerptClamp(t *testing.T) {
	if out := alCSSFor(&Props{ExcerptLines: 0}); strings.Contains(out, "-webkit-line-clamp") {
		t.Errorf("行数 0 不该输出截断段:\n%s", out)
	}
	out := alCSSFor(&Props{ExcerptLines: 2})
	if !strings.Contains(out, "-webkit-line-clamp: 2") {
		t.Errorf("行数 2 的截断声明缺失:\n%s", out)
	}
}

func TestArticleListPropsNormalize(t *testing.T) {

	if got := effectiveLimit(nil); got != 6 {
		t.Errorf("nil props 应取缺省 6，实得 %d", got)
	}
	if got := effectiveLimit(&Props{CollectionLimit: 61}); got != 60 {
		t.Errorf("超上限应封顶 60，实得 %d", got)
	}
	if got := effectiveLimit(&Props{CollectionLimit: -1}); got != 6 {
		t.Errorf("负数应回缺省 6，实得 %d", got)
	}

	if got := effectiveColumns(&Props{Layout: LayoutList, Columns: "4"}); got != "1" {
		t.Errorf("列表布局列数应恒为 1，实得 %q", got)
	}
	if got := effectiveColumns(&Props{Columns: "4"}); got != "4" {
		t.Errorf("白名单列数 4 应保留，实得 %q", got)
	}
	if got := effectiveColumns(&Props{Columns: "9"}); got != "auto" {
		t.Errorf("非法列数应回落 auto，实得 %q", got)
	}

	if got := gridTemplateColumns(LayoutGrid, "auto"); !strings.Contains(got, "min(100%, 16rem)") {
		t.Errorf("自适应列模板缺少视口封顶: %q", got)
	}
	if got := gridTemplateColumns(LayoutList, "3"); got != "1fr" {
		t.Errorf("列表布局列模板应恒为 1fr，实得 %q", got)
	}

	if got := effectiveTitleTag(&Props{TitleTag: "h5"}); got != "h3" {
		t.Errorf("非法标题标签应回落 h3，实得 %s", got)
	}
	if got := effectiveTitleTag(&Props{TitleTag: "h2"}); got != "h2" {
		t.Errorf("h2 应保留，实得 %s", got)
	}

	if got := effectiveLines(&Props{ExcerptLines: -1}); got != 0 {
		t.Errorf("负行数应回 0，实得 %d", got)
	}
	if got := effectiveLines(&Props{ExcerptLines: 7}); got != 6 {
		t.Errorf("行数应封顶 6，实得 %d", got)
	}

	if got := effectiveLinkPrefix(&Props{}); got != "/" {
		t.Errorf("链接前缀缺省应为 /，实得 %q", got)
	}
}

func TestArticleListValidate(t *testing.T) {
	c := &Component{}

	if err := c.Validate(alNodeOf(t, &Props{}), map[string]bool{}); err != nil {
		t.Errorf("空数据源应合法: %v", err)
	}
	if err := c.Validate(alNodeOf(t, &Props{CollectionSource: SourceArticle}), map[string]bool{}); err != nil {
		t.Errorf("content:article 应合法: %v", err)
	}
	if err := c.Validate(alNodeOf(t, &Props{CollectionSource: "content:product"}), map[string]bool{}); err == nil {
		t.Error("不支持的数据源应报错")
	}
	if err := c.Validate(alNodeOf(t, &Props{CollectionLimit: 61}), map[string]bool{}); err == nil {
		t.Error("取几条超上限应报错")
	}
}

type fakeArticles struct{}

func (fakeArticles) ResolveCollection(_ context.Context, source string, _ map[string]string) ([]map[string]any, error) {
	if source != SourceArticle {
		return nil, fmt.Errorf("未知集合源 %q", source)
	}
	return []map[string]any{
		{"id": 1, "slug": "first", "title": "第一篇", "excerpt": "摘要一", "featuredImage": "https://x/1.jpg"},
		{"id": 2, "slug": "second", "title": "第二篇", "excerpt": "摘要二"},
		{"id": 3, "slug": "third", "title": "第三篇"},
	}, nil
}

func TestArticleListBuildView(t *testing.T) {

	v, err := BuildView(alNodeOf(t, &Props{}), &Props{}, nil)
	if err != nil {
		t.Fatalf("空源不该报错: %v", err)
	}
	if !v.Empty || v.EmptyText != "暂无文章" {
		t.Errorf("空源应渲染空态缺省文案，实得 Empty=%v text=%q", v.Empty, v.EmptyText)
	}

	ctx := &core.RenderContext{Collection: fakeArticles{}}
	v, err = BuildView(alNodeOf(t, &Props{CollectionSource: SourceArticle}),
		&Props{CollectionSource: SourceArticle}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Empty || len(v.Cards) != 3 {
		t.Fatalf("应有 3 张卡片，实得 Empty=%v cards=%d", v.Empty, len(v.Cards))
	}
	c0 := v.Cards[0]
	if c0.Title != "第一篇" || c0.Href != "/first" || !c0.HasImage || !c0.HasExcerpt {
		t.Errorf("卡片字段映射错: %+v", c0)
	}

	if v.Cards[1].HasImage || !v.Cards[1].HasExcerpt {
		t.Errorf("第二篇应无图有摘要: %+v", v.Cards[1])
	}
	if v.Cards[2].HasExcerpt {
		t.Errorf("第三篇应无摘要: %+v", v.Cards[2])
	}

	v, err = BuildView(alNodeOf(t, &Props{CollectionSource: SourceArticle, CollectionLimit: 2}),
		&Props{CollectionSource: SourceArticle, CollectionLimit: 2}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(v.Cards) != 2 || v.Cards[0].Title != "第一篇" {
		t.Errorf("截断应保留源序前 2 条: %+v", v.Cards)
	}

	v, err = BuildView(alNodeOf(t, &Props{CollectionSource: SourceArticle, OrderBy: "oldest"}),
		&Props{CollectionSource: SourceArticle, OrderBy: "oldest"}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Cards[0].Title != "第三篇" {
		t.Errorf("oldest 应反转源序，首条实得 %q", v.Cards[0].Title)
	}
}
