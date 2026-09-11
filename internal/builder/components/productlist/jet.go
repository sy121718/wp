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

	// —— 分页（issue #27）——
	//
	// 口径：分页在集合源单次上限（100 条）以内生效 —— 组件按「每页条数 × 页码」在已取回
	// 的集合项里切片；超出上限的部分需要集合源支持 offset（票里记为后续）。
	// Page 当前页（1 起）；PageSize 0 表示不分页（此时模板不输出分页控件）。
	Page     int
	PageSize int
	// HasPrev / HasNext 是否还有上一页 / 下一页（按已取回条数判断，不猜测未取回的部分）。
	HasPrev bool
	HasNext bool
	// FetchedTotal 本次实际取回并参与分页的条数（上限 100）。
	FetchedTotal int

	// —— 筛选栏与工具条（issue #27）——
	//
	// FilterOptions 集合源给出的可选筛选项（未提供 = 该块不渲染）。
	FilterOptions core.CollectionFilterOptions
	// Show* 实际渲染出来的块（作者勾选 + 集合源确实给了值，两者都满足才为真）。
	ShowCategories bool
	ShowBrands     bool
	ShowTags       bool
	ShowAttributes bool
	ShowSort       bool
	ShowPageSize   bool
	ShowColumns    bool
	ShowOnSale     bool
	// FilterSections 筛选栏各块与其选项（含每个选项的降级链接 / 片段请求 / 推送 URL）。
	FilterSections  []FilterSection
	SortOptions     []ControlOption
	PageSizeOptions []ControlOption
	ColumnOptions   []ControlOption
	OnSaleOptions   []ControlOption

	// PrevPage / NextPage 上下页页码（HasPrev / HasNext 为假时前端不用）。
	PrevPage int
	NextPage int
	// HasPager 是否渲染分页控件（pageSize > 0 且真的分了页）。
	HasPager bool
	// PrevLink / NextLink 上下页控件（含降级链接 / 片段请求 / 推送 URL）。
	PrevLink ControlOption
	NextLink ControlOption

	// FragmentQuery 构建期拼好的实例配置（片段请求的固定部分，不进 URL）。
	FragmentQuery string
	// PushQuery 当前语义参数（片段渲染时才有；构建期为空 = 默认态）。
	PushQuery string
}

// collectionFilterOptionsProvider 能力探测：上下文里的集合解析器能否给出可选筛选项。
func collectionFilterOptionsProvider(ctx *core.RenderContext) (core.CollectionFilterOptionsProvider, bool) {
	if ctx == nil || ctx.Collection == nil {
		return nil, false
	}
	provider, ok := ctx.Collection.(core.CollectionFilterOptionsProvider)
	return provider, ok
}

// toolbarWanted 工具条是否勾选了某项。
func toolbarWanted(p *Props, name string) bool {
	for _, item := range splitList(p.Toolbar) {
		if item == name {
			return true
		}
	}
	return false
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
	// 分页：先截到「本页末」，再取本页那段。顺序不能反 ——
	// 反了的话第 2 页会拿到「全部条目里的第 pageSize+1 条开始」，但总数判断却是截断后的，
	// 于是最后一页之后还会多出一页空列表。
	fetched := len(items)
	page, pageSize := EffectivePage(p), EffectivePageSize(p)
	if pageSize > 0 {
		start := (page - 1) * pageSize
		switch {
		case start >= fetched:
			items = items[:0]
		default:
			items = items[start:]
			if len(items) > pageSize {
				items = items[:pageSize]
			}
		}
	}

	// 交互控件（筛选栏 / 工具条 / 分页）都在这里一次性拼好：模板只负责把字符串放进
	// hx-get / hx-push-url / href —— 拼 URL 的逻辑要能被单测覆盖，所以不放模板里。
	lc := newLinkContext(node.ID, p, ctx)

	view := View{
		Cards:        make([]CardView, 0, len(items)),
		Empty:        len(items) == 0 && page <= 1,
		EmptyText:    effectiveEmptyText(p),
		List:         effectiveLayout(p) == LayoutList,
		Page:         page,
		PageSize:     pageSize,
		FetchedTotal: fetched,
	}
	// 翻页可达性按**已取回条数**判断：不够就说明这一页之后没有更多了（不猜未取回的部分）。
	if pageSize > 0 {
		view.HasPrev = page > 1
		view.HasNext = page*pageSize < fetched
		view.HasPager = true
		if view.HasPrev {
			view.PrevPage = page - 1
			view.PrevLink = controlOption(lc, "上一页", false, pageOverride(view.PrevPage))
		}
		if view.HasNext {
			view.NextPage = page + 1
			view.NextLink = controlOption(lc, "下一页", false, pageOverride(view.NextPage))
		}
	}
	for _, item := range items {
		view.Cards = append(view.Cards, cardViewOf(p, item))
	}

	view.FragmentQuery = lc.instanceQuery
	view.PushQuery = lc.pushQuery
	// 筛选选项按能力探测取：集合源没实现该能力 → 筛选栏不渲染（列表本身照常可用，
	// 契约缺失不阻断构建，与 CollectionSchemaProvider 的处理一致）；取选项失败同理。
	if provider, ok := collectionFilterOptionsProvider(ctx); ok && len(splitList(p.Filters)) > 0 {
		if options, oerr := provider.CollectionFilterOptions(ctx.Context, ctx.ProjectID); oerr == nil {
			view.FilterOptions = options
		}
	}
	view.FilterSections = buildFilterSections(p, ctx, lc, &view)
	if toolbarWanted(p, "sort") {
		view.SortOptions = buildSortOptions(p, lc, &view)
	}
	if toolbarWanted(p, "pageSize") {
		view.PageSizeOptions = buildPageSizeOptions(p, lc, &view)
	}
	if toolbarWanted(p, "columns") {
		view.ColumnOptions = buildColumnOptions(p, lc, &view)
	}
	if toolbarWanted(p, "onSale") {
		view.OnSaleOptions = buildOnSaleOptions(p, lc, &view)
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
