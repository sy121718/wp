package articlelist

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

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

type View struct {
	Layout       string
	GridTemplate string
	TitleTag     string
	Label        string
	Cards        []CardView
	Empty        bool
	EmptyText    string
}

func BuildView(node *core.Node, p *Props, ctx *core.RenderContext) (View, error) {
	v := View{
		Layout:   effectiveLayout(p),
		TitleTag: effectiveTitleTag(p),
		Label:    effectiveLabel(p),
	}
	v.GridTemplate = gridTemplateColumns(v.Layout, effectiveColumns(p))
	source := ""
	if p != nil {
		source = strings.TrimSpace(p.CollectionSource)
	}
	if source == "" {

		v.Empty = true
		v.EmptyText = effectiveEmptyText(p)
		return v, nil
	}

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

func sortArticles(items []map[string]any, order string) {
	switch order {
	case "oldest":
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	case "newest", "":

	default:

	}
}

func effectiveLabel(p *Props) string {
	if p != nil {
		if t := strings.TrimSpace(p.Label); t != "" {
			return t
		}
	}
	return defaultLabel
}

func effectiveLinkPrefix(p *Props) string {
	if p == nil {
		return "/"
	}
	if t := strings.TrimSpace(p.LinkPrefix); t != "" {
		return t
	}
	return "/"
}

func effectiveEmptyText(p *Props) string {
	if p != nil {
		if t := strings.TrimSpace(p.EmptyText); t != "" {
			return t
		}
	}
	return defaultEmptyText
}
