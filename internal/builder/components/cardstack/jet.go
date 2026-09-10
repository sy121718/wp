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
}

// BuildView 生成渲染视图；内容集合模式需要 ctx.Collection（装配层注入）。
func BuildView(node *core.Node, p *Props, ctx *core.RenderContext) (View, error) {
	drag := effectiveTrigger(p) == TriggerDrag
	if source := collectionSource(p); source != "" {
		cards, err := collectionCards(node, p, ctx)
		if err != nil {
			return View{}, err
		}
		return View{Cards: cards, Collection: true, Drag: drag}, nil
	}

	n := cardCount(node, p)
	cards := make([]CardView, n)
	for i := range cards {
		label := strconv.Itoa(i + 1)
		cards[i] = CardView{Label: label, AriaLabel: "放大第 " + label + " 张卡片"}
	}
	return View{Cards: cards, HasContent: hasContent(node), Drag: drag}, nil
}

// collectionCards 解析内容集合 → 每项一张卡（字段映射取自 props）。
func collectionCards(node *core.Node, p *Props, ctx *core.RenderContext) ([]CardView, error) {
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
			return buildCollectionCards(p, items), nil
		}
	}
	if err = checkFieldMapping(node.ID, p, items); err != nil {
		return nil, err
	}
	return buildCollectionCards(p, items), nil
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

// buildCollectionCards 集合项 → 卡片视图（字段映射在 props 里）。
func buildCollectionCards(p *Props, items []map[string]any) []CardView {
	cards := make([]CardView, 0, len(items))

	for _, item := range items {
		title := fieldText(item, p.CardTitleField)
		text := fieldText(item, p.CardTextField)
		meta := fieldText(item, p.CardMetaField)
		image := fieldText(item, p.CardImageField)
		href := ""
		if p.CardLinkField != "" {
			href = p.CardLinkPrefix + fieldText(item, p.CardLinkField)
		}
		// 无障碍描述：优先标题，其次「第 N 张」。
		aria := "放大第 " + strconv.Itoa(len(cards)+1) + " 张卡片"
		if title != "" {
			aria = "放大：" + title
		}
		cards = append(cards, CardView{
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
