// Package cardstack — Jet 渲染路径辅助导出：几何与样式编译在 cardstack.go，
// HTML 由 cardstack.jet 模板拼装（与其他结构型组件同构）。
package cardstack

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// CardView 单张卡片的渲染视图。
//
// 两种内容来源共用同一视图：静态模式只用 Label（数字占位卡）或子节点（内容卡），
// 内容集合模式则用下面的一组字段（由 cardstack 自己渲染卡内元素）。
type CardView struct {
	// Label 数字占位卡的序号（1~N）；内容卡与集合卡不使用。
	Label string
	// AriaLabel 放大控件的无障碍描述。
	AriaLabel string

	// —— 内容集合模式填充 ——
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

// View 卡片堆叠渲染视图（供 cardstack.jet 模板使用）。
type View struct {
	// Cards 卡片列表：长度 = 子节点数（内容卡）、集合条数（集合模式）或占位卡数量。
	Cards []CardView
	// HasContent 是否用子节点作为卡片内容。
	HasContent bool
	// Collection 是否为内容集合模式：卡片由内容条数决定，卡内元素由字段映射渲染。
	Collection bool
	// Drag 是否为拖拽旋转模式：容器输出 data-* 属性，公共增强脚本接管指针与方向键。
	Drag bool
	// Deck 是否为堆叠轮播模式：容器输出初始主卡序号，脚本接管滑动/点击/方向键切换。
	Deck bool
	// DeckIndex 初始主卡序号（取中间那张，两侧对称叠开）。
	DeckIndex int
	// DeckLoop 堆叠轮播是否循环切换（滑到头继续往前会绕回来）。
	DeckLoop bool

	// Empty 内容集合解析出 0 条（此时不渲染卡片，改渲染占位或整体隐藏）。
	Empty bool
	// EmptyText 无内容占位文案。
	EmptyText string
	// HideEmpty 无内容时隐藏整个组件（而不是显示占位）。
	HideEmpty bool
	// LinkText 卡片详情链接文案（集合字段映射模式使用）。
	LinkText string
	// Slide 是否全屏分页模式（模板据此输出页码角标，总数写进 data-total）。
	Slide bool
	// PageTotal 全屏分页的总屏数（页码分母）。
	PageTotal int
	// DeckVertical 堆叠轮播是否纵向切换（模板输出轴向，脚本据此换拖动轴与方向键）。
	DeckVertical bool
	// DeckArrows 是否输出「上一页 / 下一页」按钮。
	DeckArrows bool
	// DeckClickNext 点击卡片是否翻下一页（而不是展开放大）。
	DeckClickNext bool
	// ZoomGroup 放大用 radio 的组名（每实例独立）：多组件此前共用同一个 name，
	// 选中状态会跨组件互相取消，ARIA 上也说不清是"哪一组的单选"。
	ZoomGroup string
	// ListSemantics 是否输出列表语义（track = role="list"、卡片 = role="listitem"）。
	// deck 模式带翻页按钮与主卡切换，语义是轮播而非并列列表，故排除。
	ListSemantics bool
	// Labels 无障碍与控件文案（审计 I18N-010）：构建期由 ApplyI18n 按语言回填。
	Labels CardstackLabels

	// —— 列表页入口（系统页面槽位 blog / shop）——
	//
	// 只对能对上系统页面语义的集合源输出：文章集合 → blog（更多文章）、
	// 商品集合 → shop（全部商品）。其余集合源（分类列表等）没有对应的系统页面，恒不输出。
	// HasListPageLink 为假时模板整块不渲染 —— 未绑定或未发布都不输出死链。
	HasListPageLink  bool
	ListPageLinkHref string
	ListPageLinkText string
}

// zoomGroup 放大用 radio 的组名：以节点 ID 结尾，保证同页多个 cardstack 各自独立成组
// （此前共用 "sky-cardstack-zoom"，一个组件里选中的卡会取消另一个组件的选中态）。
func zoomGroup(nodeID string) string { return "sky-cardstack-zoom-" + nodeID }

// IsCollection 是否内容集合模式 —— 装配层据此决定「子节点模板」还是「组件自带字段映射」。
func IsCollection(p *Props) bool { return collectionSource(p) != "" }

// CollectionItems 解析集合项（含字段白名单校验与裁剪），供装配层按项展开子节点模板。
func CollectionItems(node *core.Node, p *Props, ctx *core.RenderContext) ([]map[string]any, error) {
	return resolveItems(node, p, ctx)
}

// BuildView 生成渲染视图；内容集合模式需要 ctx.Collection（装配层注入）。
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
		hasListLink, listHref, listText := collectionListPageLink(source, p, ctx)
		return View{
			Cards: cards, Collection: true,
			Drag: drag, Deck: deck, DeckIndex: len(cards) / 2, DeckLoop: p.DeckLoop,
			Empty: len(cards) == 0, EmptyText: emptyText(p), HideEmpty: p.CollectionEmpty == "hide",
			LinkText: linkText(p), Slide: slide, PageTotal: len(cards),
			DeckVertical: deckVertical, DeckArrows: p.DeckArrows, DeckClickNext: p.DeckClick == deckClickNext,
			ZoomGroup: zoomGroup(node.ID), ListSemantics: !deck, Labels: defaultCardstackLabels(),
			HasListPageLink: hasListLink, ListPageLinkHref: listHref, ListPageLinkText: listText,
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

// collectionCards 解析内容集合 → 每项一张卡（字段映射取自 props）。
func collectionCards(node *core.Node, p *Props, ctx *core.RenderContext) ([]CardView, error) {
	items, err := resolveItems(node, p, ctx)
	if err != nil {
		return nil, err
	}
	return buildCollectionCards(p, items, ctx.ResolveSiteLink), nil
}

// resolveItems 解析集合源 → 字段列表：限额、白名单校验（有元数据契约时严格校验并裁剪，
// 否则按数据实际字段判断）。子节点模板模式与组件自带字段映射模式共用它。
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

	// 字段校验两条路：
	//   1) 解析器实现了集合元数据契约（content 模块）→ 按白名单严格校验（不变量 4）；
	//   2) 没有该能力 → 退回「按数据实际字段判断」，不阻断构建。
	if provider, ok := ctx.Collection.(core.CollectionSchemaProvider); ok {
		schemas, serr := provider.CollectionSchemas(ctx.Context)
		if serr == nil {
			if err = checkFieldsAgainstSchema(node.ID, p, schemas); err != nil {
				return nil, err
			}
			// 按白名单裁剪：模板只能渲染声明字段（不变量 4）。
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

// checkFieldsAgainstSchema 按集合元数据白名单校验字段映射。
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
		// slug/id/revision 是集合项的系统字段，始终可用。
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

// cropBySchema 按白名单裁剪集合项字段（不变量 4：模板只能渲染声明字段）。
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

// localizedLinkPrefix 作者填的链接前缀 → 当前语言的访问路径（审计 I18N-015，可空解析器）。
//
// 只本地化**前缀**，不碰字段值：前缀是作者在组件上填的站内逻辑路径（/article/），
// 字段值是 CMS 数据（纯 slug），再前缀一次会指到不存在的地址 —— 与 button 的
// ActionLink「CMS 绑定值不本地化」同一条口径。
//
// 本组件按「前缀 + 字段值」直接拼接，尾斜杠是分隔符：本地化会规范化路径（去掉尾斜杠），
// 不按原前缀的形状补回来的话，/article/ 会被拼成 /en/articlefirst。
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

// buildCollectionCards 集合项 → 卡片视图（字段映射在 props 里）。
//
// siteLink 站内链接本地化器（审计 I18N-015，可空）：CardLinkPrefix 是作者填的站内逻辑路径。
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
		// 无障碍描述：优先标题，其次「第 N 张」。
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

// linkText 卡片详情链接文案（props 覆盖内置缺省）。
func linkText(p *Props) string {
	if t := strings.TrimSpace(p.CardLinkText); t != "" {
		return t
	}
	return defaultCardLinkText
}

// emptyText 无内容占位文案（props 覆盖内置缺省）。
func emptyText(p *Props) string {
	if t := strings.TrimSpace(p.CollectionEmptyText); t != "" {
		return t
	}
	return defaultCollectionEmptyText
}

// checkFieldMapping 校验字段映射：字段名写错时立刻报错并列出该集合的可用字段，
// 而不是静默渲染成空白。
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

// fieldText 取字段值并转成展示文本；数组取首元素（如 product.images）。
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
		// JSON 数字：整数值去掉小数尾巴（价格 299 而非 299.000000）。
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

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：卡片堆叠的三种交互形态
// 各自输出一组 data-* 属性（cardstack.jet 的根 div / 轨道 / 翻页按钮），客户端增强按它们接管
// 拖拽、堆叠轮播与全屏分页 —— 登记漏一个，对应的交互就整个失效且页面看不出异常。
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
