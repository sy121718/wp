// Package productlist — Jet 渲染路径（视图组装）。
//
// 取值口径与 core.productCard 完全一致：图片取首元素、标签解析 JSON 名称数组、
// 链接按前缀拼接并过协议白名单 —— 三件事都直接复用 productcard 的导出函数，
// 不做第二份实现（两套口径一旦漂移，同一商品在卡片与列表里会显示成两个样）。
package productlist

import (
	"fmt"
	"sort"
	"strings"
	"time"

	productcard "go_wp/internal/builder/components/productcard"
	"go_wp/internal/builder/core"
)

// CompileCSS 商品列表样式编译（jetview 经本入口调用，实现仍在 productlist.go）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// CardView 单个商品的卡片视图（字段有值才输出对应节点）。
type CardView struct {
	TitleTag string

	HasImage bool
	ImageURL string
	ImageAlt string

	HasTitle bool
	Title    string
	Href     string

	HasPrice        bool
	Price           string
	HasComparePrice bool
	ComparePrice    string

	Tags []string
}

// View 商品列表渲染视图（供 product_list.jet 使用）。
type View struct {
	Cards []CardView
	// Empty 集合解析出 0 条（渲染空态文案，而不是空白块）。
	Empty     bool
	EmptyText string
	// List 是否列表布局（模板据此输出额外类名；布局样式由 CSS 编译期给出）。
	List bool
}

// IsCollection 本组件恒为集合模式（取数来自集合源）。
//
// 与 cardstack 的同名函数语义不同：后者是「集合 / 静态卡片」二选一，本组件只有集合一种。
// 这里保留这个函数是为了让装配层的判定方式统一（读起来是同一件事）。
func IsCollection(_ *Props) bool { return true }

// BuildView 解析集合 → 排序 → 截断 → 映射成卡片视图。
//
// 顺序是刻意的：先排序再截断。反过来会出现「取最新 12 条」先按默认序截断、
// 结果拿到的是最早那 12 条（典型的静默错序）。
func BuildView(node *core.Node, p *Props, ctx *core.RenderContext) (View, error) {
	source := effectiveSource(p)
	if source == "" {
		// 未选择数据源：渲染空态，**不碰集合解析器** —— 刚拖出来还没挑源时不查库、也不该编译失败。
		return View{Empty: true, EmptyText: effectiveEmptyText(p), List: effectiveLayout(p) == LayoutList},
			nil
	}
	if ctx == nil || ctx.Collection == nil {
		return View{}, fmt.Errorf("节点 %s: 编译上下文缺少集合解析器（无法解析商品集合 %s）", node.ID, source)
	}
	items, err := ctx.Collection.ResolveCollection(ctx.Context, source, collectionFilter(p))
	if err != nil {
		return View{}, fmt.Errorf("节点 %s: 商品集合解析失败: %w", node.ID, err)
	}
	sortItems(items, effectiveOrder(p))
	if limit := effectiveLimit(p); len(items) > limit {
		items = items[:limit]
	}

	view := View{
		Cards:     make([]CardView, 0, len(items)),
		Empty:     len(items) == 0,
		EmptyText: effectiveEmptyText(p),
		List:      effectiveLayout(p) == LayoutList,
	}
	for _, item := range items {
		view.Cards = append(view.Cards, cardViewOf(p, item))
	}
	return view, nil
}

// cardViewOf 把一条集合项映射成卡片视图（逐槽位取值，空值不输出节点）。
func cardViewOf(p *Props, item map[string]any) CardView {
	view := CardView{TitleTag: effectiveTitleTag(p)}
	var rawImageAlt, rawTitle string
	for _, s := range p.slotFields() {
		value := strings.TrimSpace(itemField(item, s.Field))
		if value == "" {
			continue
		}
		switch s.Slot {
		case slotImage:
			if url := productcard.FirstImageURL(value); url != "" {
				view.HasImage, view.ImageURL = true, url
			}
		case slotImageAlt:
			rawImageAlt = value
		case slotTitle:
			view.HasTitle, view.Title = true, value
			rawTitle = value
		case slotPrice:
			view.HasPrice, view.Price = true, effectiveCurrency(p)+value
		case slotComparePrice:
			view.HasComparePrice, view.ComparePrice = true, effectiveCurrency(p)+value
		case slotTags:
			view.Tags = productcard.ParseTagNames(value)
		case slotLink:
			view.Href = productcard.CardHref(p.LinkPrefix, value)
		}
	}
	// 主图 alt：作者字段优先，缺失回退标题（仅当确有图）。
	if view.HasImage {
		view.ImageAlt = rawImageAlt
		if view.ImageAlt == "" {
			view.ImageAlt = rawTitle
		}
	}
	return view
}

// itemField 取集合项字段值。
//
// 槽位写的 item.<字段> 与 product.<字段> 在这里都按**当前商品项**取值：列表组件里
// 每一项就是当前商品，两种写法表达的是同一件事。宽容处理是有意的 —— 作者把商品卡里
// 的 product.* 配置复制到列表组件上时，不该因为前缀不同而整列变空。
func itemField(item map[string]any, field string) string {
	name := strings.TrimSpace(field)
	if name == "" {
		return ""
	}
	if _, tail, ok := strings.Cut(name, "."); ok {
		name = tail
	}
	return core.ItemFieldText(item, name)
}

// sortItems 按 props 声明的口径重排（默认保持集合源的确定性序：排序号 → 创建时间 → id）。
//
// 时间解析失败的行按零值参与比较（排到最后）而不是丢弃：缺 createdAt 不该让商品消失。
func sortItems(items []map[string]any, order string) {
	if order == OrderDefault || len(items) < 2 {
		return
	}
	createdAt := func(item map[string]any) time.Time {
		s, _ := item["createdAt"].(string)
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
		if err != nil {
			return time.Time{}
		}
		return t
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := createdAt(items[i]), createdAt(items[j])
		if order == OrderNewest {
			return a.After(b)
		}
		return a.Before(b)
	})
}
