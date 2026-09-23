package articlelist

// jet.go — Jet 渲染路径：集合解析 → 排序 → 截断 → 卡片视图。

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// CardView 单张文章卡片的渲染数据。
type CardView struct {
	Image      string
	Alt        string
	HasImage   bool
	Title      string
	Excerpt    string
	HasExcerpt bool
	Href       string
	HasHref    bool
}

// View 文章列表渲染视图。
type View struct {
	Layout       string
	GridTemplate string
	TitleTag     string
	LinkText     string
	Cards        []CardView
	Empty        bool
	EmptyText    string
}

// BuildView 解析集合 → 排序 → 截断 → 映射成卡片视图。
//
// 顺序是刻意的：先排序再截断。反过来会变成「取最新 6 条」先按默认序截断、
// 结果拿到的是最早那 6 条（典型的静默错序）。
func BuildView(node *core.Node, p *Props, ctx *core.RenderContext) (View, error) {
	v := View{
		Layout:   effectiveLayout(p),
		TitleTag: effectiveTitleTag(p),
		LinkText: effectiveLinkText(p),
	}
	v.GridTemplate = gridTemplateColumns(v.Layout, effectiveColumns(p))
	source := ""
	if p != nil {
		source = strings.TrimSpace(p.CollectionSource)
	}
	if source == "" {
		// 未选择数据源：渲染空态，**不碰集合解析器** ——
		// 刚拖出来还没挑源时不查库、也不该让整页编译失败。
		v.Empty = true
		v.EmptyText = effectiveEmptyText(p)
		return v, nil
	}
	// 专用组件优先用**业务侧声明的受限数据源**（issue #35：只有读集合 / 元数据 /
	// 可筛值，写方法不在接口上）；未注入时退回通用集合注册表（兼容单测与渐进切换）。
	if ctx == nil {
		return View{}, fmt.Errorf("节点 %s: 编译上下文为空", node.ID)
	}
	resolver := core.CollectionResolver(nil)
	switch {
	case ctx.ContentSource != nil:
		resolver = ctx.ContentSource
	case ctx.Collection != nil:
		resolver = ctx.Collection
	default:
		return View{}, fmt.Errorf("节点 %s: 编译上下文缺少内容数据源（无法解析集合 %s）", node.ID, source)
	}
	items, err := resolver.ResolveCollection(ctx.Context, source, nil)
	if err != nil {
		return View{}, fmt.Errorf("节点 %s: 集合解析失败: %w", node.ID, err)
	}
	sortArticles(items, strings.TrimSpace(p.OrderBy))
	if limit := effectiveLimit(p); len(items) > limit {
		items = items[:limit]
	}
	if len(items) == 0 {
		v.Empty = true
		v.EmptyText = effectiveEmptyText(p)
		return v, nil
	}
	// 站内链接本地化（与 cardstack 同源）：LinkPrefix 是作者填的站内逻辑路径，
	// 多语言前缀在构建期由本地化器加上。
	prefix := effectiveLinkPrefix(p)
	prefix = ctx.ResolveSiteLink(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for _, item := range items {
		title := itemText(item, "title")
		slug := itemText(item, "slug")
		v.Cards = append(v.Cards, CardView{
			Image:      itemText(item, "featuredImage"),
			Alt:        title,
			HasImage:   strings.TrimSpace(itemText(item, "featuredImage")) != "",
			Title:      title,
			Excerpt:    itemText(item, "excerpt"),
			HasExcerpt: strings.TrimSpace(itemText(item, "excerpt")) != "",
			Href:       prefix + slug,
			HasHref:    slug != "",
		})
	}
	return v, nil
}

// itemText 取集合项里的字符串字段（缺失 / 非字符串 → 空串）。
func itemText(item map[string]any, field string) string {
	if item == nil {
		return ""
	}
	v, ok := item[field]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// sortArticles 按 orderBy 排序（默认保持集合源的确定性序）。
//
// 集合源给的是「更新时间倒序」，所以 newest 是恒等、oldest 才需要反转 ——
// 不在这里重新发明一套排序口径，避免与列表页的默认序分叉。
func sortArticles(items []map[string]any, order string) {
	switch order {
	case "oldest":
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	case "newest", "":
		// 集合源默认即「更新时间倒序」，恒等。
	default:
		// 未知取值：保持源序（不 panic —— 作者的旧配置不该让整页构建失败）。
	}
}

// effectiveLinkText 卡片链接文案。
func effectiveLinkText(p *Props) string {
	if p != nil {
		if t := strings.TrimSpace(p.LinkText); t != "" {
			return t
		}
	}
	return defaultLinkText
}

// effectiveLinkPrefix 链接前缀（默认 "/"，与默认的 article 路径模式 "/{slug}" 一致）。
func effectiveLinkPrefix(p *Props) string {
	if p == nil {
		return "/"
	}
	if t := strings.TrimSpace(p.LinkPrefix); t != "" {
		return t
	}
	return "/"
}

// effectiveEmptyText 空态文案。
func effectiveEmptyText(p *Props) string {
	if p != nil {
		if t := strings.TrimSpace(p.EmptyText); t != "" {
			return t
		}
	}
	return defaultEmptyText
}
