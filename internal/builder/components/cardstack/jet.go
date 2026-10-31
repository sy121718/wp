package cardstack

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

type CardView struct {
	Label string

	AriaLabel string

	Title    string
	Text     string
	Meta     string
	Image    string
	Href     string
	LinkText string
	HasImage bool
	HasTitle bool
	HasText  bool
	HasMeta  bool
	HasHref  bool
}

type View struct {
	Cards []CardView

	HasContent bool

	Collection bool

	Drag bool

	Deck bool

	DeckIndex int

	DeckLoop bool

	Empty bool

	EmptyText string

	HideEmpty bool

	LinkText string

	Slide bool

	PageTotal int

	DeckVertical bool

	DeckArrows bool

	PrevIcon  string
	NextIcon  string
	CloseIcon string

	DeckClickNext bool

	ZoomGroup string

	ListSemantics bool

	Labels CardstackLabels

	HasListPageLink  bool
	ListPageLinkHref string
	ListPageLinkText string
	ListPageNewTab   bool
}

func zoomGroup(nodeID string) string { return "sky-cardstack-zoom-" + nodeID }

func IsCollection(p *Props) bool { return collectionSource(p) != "" }

func CollectionItems(node *core.Node, p *Props, ctx *core.RenderContext) ([]map[string]any, error) {
	return resolveItems(node, p, ctx)
}

func (v View) PrevIconSVG() string {
	svg, _ := core.IconSVGClass("chevron-left", "sky-cardstack-arrow-icon")
	return svg
}

func (v View) NextIconSVG() string {
	svg, _ := core.IconSVGClass("chevron-right", "sky-cardstack-arrow-icon")
	return svg
}

func (v View) CloseIconSVG() string {
	svg, _ := core.IconSVGClass("close", "sky-cardstack-close-icon")
	return svg
}

func BuildView(node *core.Node, p *Props, ctx *core.RenderContext) (View, error) {
	trigger := effectiveTrigger(p)
	drag := trigger == TriggerDrag
	deck := trigger == TriggerDeck
	slide := trigger == TriggerSlide
	deckVertical := deck && effectiveDeckDirection(p) == "vertical"
	if source := collectionSource(p); source != "" {
		cards, err := collectionCards(node, p, ctx)
		if err != nil {
			return View{}, err
		}
		action, listText, err := collectionListPageLink(node, source, p, ctx)
		if err != nil {
			return View{}, err
		}
		return View{
			Cards: cards, Collection: true,
			Drag: drag, Deck: deck, DeckIndex: len(cards) / 2, DeckLoop: p.DeckLoop,
			Empty: len(cards) == 0, EmptyText: emptyText(p), HideEmpty: p.CollectionEmpty == "hide",
			LinkText: linkText(p), Slide: slide, PageTotal: len(cards),
			DeckVertical: deckVertical, DeckArrows: p.DeckArrows, DeckClickNext: p.DeckClick == deckClickNext,
			ZoomGroup: zoomGroup(node.ID), ListSemantics: !deck, Labels: defaultCardstackLabels(),
			HasListPageLink: action.URL != "", ListPageLinkHref: action.URL, ListPageLinkText: listText, ListPageNewTab: action.NewTab,
		}, nil
	}

	n := cardCount(node, p)
	cards := make([]CardView, n)
	for i := range cards {
		label := strconv.Itoa(i + 1)
		cards[i] = CardView{Label: label, AriaLabel: "放大第 " + label + " 张卡片"}
	}
	return View{
		Cards: cards, HasContent: hasContent(node),
		Drag: drag, Deck: deck, DeckIndex: len(cards) / 2, DeckLoop: p.DeckLoop,
		Slide: slide, PageTotal: len(cards),
		DeckVertical: deckVertical, DeckArrows: p.DeckArrows, DeckClickNext: p.DeckClick == deckClickNext,
		ZoomGroup: zoomGroup(node.ID), ListSemantics: !deck, Labels: defaultCardstackLabels(),
	}, nil
}

func collectionCards(node *core.Node, p *Props, ctx *core.RenderContext) ([]CardView, error) {
	items, err := resolveItems(node, p, ctx)
	if err != nil {
		return nil, err
	}
	return buildCollectionCards(p, items, ctx.ResolveSiteLink), nil
}

func resolveItems(node *core.Node, p *Props, ctx *core.RenderContext) ([]map[string]any, error) {
	if ctx == nil || ctx.Collection == nil {
		return nil, fmt.Errorf("节点 %s: 编译上下文缺少集合解析器（无法解析内容集合 %q）", node.ID, p.CollectionSource)
	}
	items, err := ctx.Collection.ResolveCollection(ctx.Context, p.CollectionSource, nil)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: 内容集合 %q 解析失败: %w", node.ID, p.CollectionSource, err)
	}
	limit := effectiveCollectionLimit(p)
	if len(items) > limit {
		items = items[:limit]
	}

	if provider, ok := ctx.Collection.(core.CollectionSchemaProvider); ok {
		schemas, serr := provider.CollectionSchemas(ctx.Context)
		if serr == nil {
			if err = checkFieldsAgainstSchema(node.ID, p, schemas); err != nil {
				return nil, err
			}

			items = cropBySchema(items, schemas, p)
			if err = checkFieldMapping(node.ID, p, items); err != nil {
				return nil, err
			}
			return items, nil
		}
	}
	if err = checkFieldMapping(node.ID, p, items); err != nil {
		return nil, err
	}
	return items, nil
}

func checkFieldsAgainstSchema(nodeID string, p *Props, schemas []core.CollectionSchema) error {
	var schema *core.CollectionSchema
	available := make([]string, 0, len(schemas))
	for i := range schemas {
		available = append(available, schemas[i].Source)
		if schemas[i].Source == p.CollectionSource {
			schema = &schemas[i]
		}
	}
	if schema == nil {
		return fmt.Errorf("节点 %s: 未知集合源 %q（可用：%s）", nodeID, p.CollectionSource, strings.Join(available, "、"))
	}
	allow := make(map[string]bool, len(schema.Fields))
	for _, f := range schema.Fields {
		allow[f] = true
	}
	for _, f := range []struct{ label, name string }{
		{"图片字段", p.CardImageField},
		{"标题字段", p.CardTitleField},
		{"正文字段", p.CardTextField},
		{"附注字段", p.CardMetaField},
		{"链接字段", p.CardLinkField},
	} {
		if f.name == "" {
			continue
		}

		if f.name == "slug" || f.name == "id" || f.name == "revision" {
			continue
		}
		if !allow[f.name] {
			return fmt.Errorf("节点 %s: %s %q 不在集合 %q 的字段白名单内（可用：%s、slug）",
				nodeID, f.label, f.name, p.CollectionSource, strings.Join(schema.Fields, "、"))
		}
	}
	return nil
}

func cropBySchema(items []map[string]any, schemas []core.CollectionSchema, p *Props) []map[string]any {
	var fields []string
	for i := range schemas {
		if schemas[i].Source == p.CollectionSource {
			fields = schemas[i].Fields
		}
	}
	allow := map[string]bool{"slug": true, "id": true, "revision": true}
	for _, f := range fields {
		allow[f] = true
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := make(map[string]any, len(item))
		for k, v := range item {
			if allow[k] {
				row[k] = v
			}
		}
		out = append(out, row)
	}
	return out
}

func localizedLinkPrefix(prefix string, siteLink func(string) string) string {
	if strings.TrimSpace(prefix) == "" {
		return prefix
	}
	localized := core.SiteLinkOrSame(siteLink, prefix)
	if strings.HasSuffix(prefix, "/") && !strings.HasSuffix(localized, "/") {
		localized += "/"
	}
	return localized
}

func buildCollectionCards(p *Props, items []map[string]any, siteLink func(string) string) []CardView {
	cards := make([]CardView, 0, len(items))

	for _, item := range items {
		title := fieldText(item, p.CardTitleField)
		text := fieldText(item, p.CardTextField)
		meta := fieldText(item, p.CardMetaField)
		image := fieldText(item, p.CardImageField)
		href := ""
		if p.CardLinkField != "" {
			href = strings.TrimSpace(localizedLinkPrefix(p.CardLinkPrefix, siteLink) + fieldText(item, p.CardLinkField))
		}

		aria := "放大第 " + strconv.Itoa(len(cards)+1) + " 张卡片"
		if title != "" {
			aria = "放大：" + title
		}
		cards = append(cards, CardView{
			LinkText:  linkText(p),
			AriaLabel: aria,
			Title:     title, HasTitle: title != "",
			Text: text, HasText: text != "",
			Meta: meta, HasMeta: meta != "",
			Image: image, HasImage: image != "",
			Href: href, HasHref: href != "",
		})
	}
	return cards
}

func linkText(p *Props) string {
	if t := strings.TrimSpace(p.CardLinkText); t != "" {
		return t
	}
	return defaultCardLinkText
}

func emptyText(p *Props) string {
	if t := strings.TrimSpace(p.CollectionEmptyText); t != "" {
		return t
	}
	return defaultCollectionEmptyText
}

func checkFieldMapping(nodeID string, p *Props, items []map[string]any) error {
	if len(items) == 0 {
		return nil
	}
	sample := items[0]
	keys := make([]string, 0, len(sample))
	for k := range sample {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, f := range []struct{ label, name string }{
		{"图片字段", p.CardImageField},
		{"标题字段", p.CardTitleField},
		{"正文字段", p.CardTextField},
		{"附注字段", p.CardMetaField},
		{"链接字段", p.CardLinkField},
	} {
		if f.name == "" {
			continue
		}
		if _, ok := sample[f.name]; !ok {
			return fmt.Errorf("节点 %s: %s %q 在集合 %q 里不存在（可用字段：%s）",
				nodeID, f.label, f.name, p.CollectionSource, strings.Join(keys, "、"))
		}
	}
	return nil
}

func fieldText(item map[string]any, field string) string {
	if field == "" {
		return ""
	}
	v, ok := item[field]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) == 0 {
			return ""
		}
		return fieldText(map[string]any{"v": t[0]}, "v")
	case float64:

		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "是"
		}
		return "否"
	default:
		return fmt.Sprint(t)
	}
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.Slide {
		attrs = append(attrs, "data-cardstack-slide", "data-total")
	}
	if v.Drag {
		attrs = append(attrs, "data-cardstack-drag")
	}
	if v.Deck {
		attrs = append(attrs, "data-cardstack-deck")
		if v.DeckClickNext {
			attrs = append(attrs, "data-deck-click")
		}
		if v.DeckLoop {
			attrs = append(attrs, "data-cardstack-loop")
		}
		if v.DeckVertical {
			attrs = append(attrs, "data-cardstack-deck-axis")
		}
	}
	attrs = append(attrs, "data-cardstack-track")
	if v.DeckArrows {
		attrs = append(attrs, "data-cardstack-prev", "data-cardstack-next")
	}
	return attrs, nil
}
