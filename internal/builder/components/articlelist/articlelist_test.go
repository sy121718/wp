package articlelist

// articlelist_test.go — 文章列表组件的组件级断言（此前只有 feature 链路 + a11y 遍历罩住，
// 组件自身的 CSS 契约 / props 归一 / 视图装配没有任何就近断言）。
//
// 易碎点与 productlist 同形：
//  1. 网格列模板是模板内联的 --sky-al-cols（Go 侧算好），CSS 只消费 —— 列数归一错了，
//     页面只表现为「列数不对」，编译期没有任何报错；
//  2. 列表布局与摘要截断走规则级 @if —— 条件值算错时整块规则凭空消失。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// alCSSFor 渲染指定 Props 下的样式产物（作用域固定为 .sky-c-t）。
func alCSSFor(p *Props) string {
	var b core.CSSBuckets
	CompileCSS("t", p, &b)
	return b.String()
}

// alNodeOf 构造带 props 的节点。
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

// TestArticleListCSSContract 静态样式契约：作用域、列模板消费、窄屏单列、令牌基座。
func TestArticleListCSSContract(t *testing.T) {
	out := alCSSFor(&Props{})
	for _, want := range []string{
		".sky-c-t {",
		// 列模板只消费模板内联变量，CSS 不重复判断列数。
		"grid-template-columns: var(--sky-al-cols, repeat(3, minmax(0, 1fr)))",
		// 窄视口恒单列（多端硬规则）。
		"@media (max-width: 767px)",
		"grid-template-columns: 1fr",
		// 卡片基座走主题令牌（fallback 一致性由 builder 包的门禁测试守住）。
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

// TestArticleListCSSListVariant 列表布局的 @if isList 段：list 出、grid 不出。
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

// TestArticleListCSSExcerptClamp 摘要截断段：0 = 整段不输出，2 = 带行数输出。
func TestArticleListCSSExcerptClamp(t *testing.T) {
	if out := alCSSFor(&Props{ExcerptLines: 0}); strings.Contains(out, "-webkit-line-clamp") {
		t.Errorf("行数 0 不该输出截断段:\n%s", out)
	}
	out := alCSSFor(&Props{ExcerptLines: 2})
	if !strings.Contains(out, "-webkit-line-clamp: 2") {
		t.Errorf("行数 2 的截断声明缺失:\n%s", out)
	}
}

// TestArticleListPropsNormalize props 归一：缺省、封顶、白名单、回落。
func TestArticleListPropsNormalize(t *testing.T) {
	// 取几条：缺省 6、封顶 60、负数回缺省。
	if got := effectiveLimit(nil); got != 6 {
		t.Errorf("nil props 应取缺省 6，实得 %d", got)
	}
	if got := effectiveLimit(&Props{CollectionLimit: 61}); got != 60 {
		t.Errorf("超上限应封顶 60，实得 %d", got)
	}
	if got := effectiveLimit(&Props{CollectionLimit: -1}); got != 6 {
		t.Errorf("负数应回缺省 6，实得 %d", got)
	}
	// 列数：列表布局恒单列；网格布局白名单 2/3/4，其余回落自适应。
	if got := effectiveColumns(&Props{Layout: LayoutList, Columns: "4"}); got != "1" {
		t.Errorf("列表布局列数应恒为 1，实得 %q", got)
	}
	if got := effectiveColumns(&Props{Columns: "4"}); got != "4" {
		t.Errorf("白名单列数 4 应保留，实得 %q", got)
	}
	if got := effectiveColumns(&Props{Columns: "9"}); got != "auto" {
		t.Errorf("非法列数应回落 auto，实得 %q", got)
	}
	// 列模板：auto 档必须 min(100%, …) 封顶（大卡片 + 窄屏不横向溢出）。
	if got := gridTemplateColumns(LayoutGrid, "auto"); !strings.Contains(got, "min(100%, 16rem)") {
		t.Errorf("自适应列模板缺少视口封顶: %q", got)
	}
	if got := gridTemplateColumns(LayoutList, "3"); got != "1fr" {
		t.Errorf("列表布局列模板应恒为 1fr，实得 %q", got)
	}
	// 标题标签：白名单 h2/h3/h4，越界回落 h3。
	if got := effectiveTitleTag(&Props{TitleTag: "h5"}); got != "h3" {
		t.Errorf("非法标题标签应回落 h3，实得 %s", got)
	}
	if got := effectiveTitleTag(&Props{TitleTag: "h2"}); got != "h2" {
		t.Errorf("h2 应保留，实得 %s", got)
	}
	// 摘要行数：负数回 0（不截断），超 6 封顶。
	if got := effectiveLines(&Props{ExcerptLines: -1}); got != 0 {
		t.Errorf("负行数应回 0，实得 %d", got)
	}
	if got := effectiveLines(&Props{ExcerptLines: 7}); got != 6 {
		t.Errorf("行数应封顶 6，实得 %d", got)
	}
	// 链接前缀缺省 "/"。
	if got := effectiveLinkPrefix(&Props{}); got != "/" {
		t.Errorf("链接前缀缺省应为 /，实得 %q", got)
	}
}

// TestArticleListValidate 校验：空源合法（中间态）、错源报错、limit 越界报错。
func TestArticleListValidate(t *testing.T) {
	c := &Component{}
	// 刚拖出来还没挑源：合法，不查库、不报错。
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

// fakeArticles 假集合源：三条文章，id 顺序即默认源序。
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

// TestArticleListBuildView 视图装配：卡片字段映射、链接拼接、截断、空态。
func TestArticleListBuildView(t *testing.T) {
	// 未选数据源：空态且不碰解析器（ctx 传 nil 也不该炸）。
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
	// 缺图缺摘要的卡片：Has 位必须跟着空（模板据此整块不渲染）。
	if v.Cards[1].HasImage || !v.Cards[1].HasExcerpt {
		t.Errorf("第二篇应无图有摘要: %+v", v.Cards[1])
	}
	if v.Cards[2].HasExcerpt {
		t.Errorf("第三篇应无摘要: %+v", v.Cards[2])
	}

	// 截断：先排序再截断（倒过来会拿错条目）。
	v, err = BuildView(alNodeOf(t, &Props{CollectionSource: SourceArticle, CollectionLimit: 2}),
		&Props{CollectionSource: SourceArticle, CollectionLimit: 2}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(v.Cards) != 2 || v.Cards[0].Title != "第一篇" {
		t.Errorf("截断应保留源序前 2 条: %+v", v.Cards)
	}

	// oldest：源序是「更新时间倒序」，oldest 即反转。
	v, err = BuildView(alNodeOf(t, &Props{CollectionSource: SourceArticle, OrderBy: "oldest"}),
		&Props{CollectionSource: SourceArticle, OrderBy: "oldest"}, ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if v.Cards[0].Title != "第三篇" {
		t.Errorf("oldest 应反转源序，首条实得 %q", v.Cards[0].Title)
	}
}
