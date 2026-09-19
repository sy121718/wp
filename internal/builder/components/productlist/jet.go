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

	"go_wp/internal/builder/source"
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

	// —— 评分块（issue #29）——
	HasRatingSection bool
	RatingOptions    []ControlOption

	// —— 价格块（issue #28）——
	HasPriceSection bool
	PriceOptions    []ControlOption
	ShowPriceSlider bool
	// PriceBoundMin / PriceBoundMax 滑块可拖区间（作者配置的展示范围，不是数据范围）。
	PriceBoundMin string
	PriceBoundMax string
	// PriceFromValue / PriceToValue 滑块与输入框的当前值（未设的端点回落到边界）。
	PriceFromValue string
	PriceToValue   string
	// PriceFragmentGet 有 JS 时的片段请求（带实例配置）。
	PriceFragmentGet string
	// PriceFormAction 无 JS 时的原生表单 action（只带语义参数）。
	PriceFormAction string
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
	// Labels 固定文案（审计 I18N-010）：构建期由 ApplyI18n 按语言回填。
	Labels ListLabels
	// PageText 「第 N 页」的成品文案（含计数，由 ApplyI18n 一次算好）。
	PageText string
	// PrevLink / NextLink 上下页控件（含降级链接 / 片段请求 / 推送 URL）。
	PrevLink ControlOption
	NextLink ControlOption

	// FragmentQuery 构建期拼好的实例配置（片段请求的固定部分，不进 URL）。
	FragmentQuery string
	// PushQuery 当前语义参数（片段渲染时才有；构建期为空 = 默认态）。
	PushQuery string

	// —— 列表页链接（系统页面槽位 shop）——
	//
	// HasListPageLink 为真才输出链接：槽位没绑、或绑了但那页没发布，ctx.SitePages 里
	// 就没有 shop 这个键，组件**不输出任何链接**（也绝不猜一个默认路径）——
	// 猜错的链接是死链，而页面作者从产物上看不出它是猜的。
	HasListPageLink  bool
	ListPageLinkHref string
	ListPageLinkText string
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
// withArchiveFilter 按归档上下文生成筛选副本（审计 EDT-004）。
//
// 不在归档上下文、未开启开关、或归档实体类型在这个组件里没有对应维度时返回 nil，
// 调用方保持原 Props。**不静默换维度**：拿不准就不筛，而不是拿别的字段凑一个条件。
func (p *Props) withArchiveFilter(ctx *core.RenderContext) *Props {
	if p == nil || ctx == nil || strings.TrimSpace(p.FilterFromArchive) != "on" {
		return nil
	}
	entityType, entityID := ctx.ArchiveEntity()
	if entityID == "" {
		return nil
	}
	clone := *p
	switch entityType {
	case "category":
		clone.FilterCategoryID = entityID
	case "brand":
		clone.FilterBrandID = entityID
	case "tag":
		clone.FilterTagIDs = entityID
	default:
		return nil
	}
	return &clone
}

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
	// 归档上下文（审计 EDT-004）：归档页里的列表用实例自己的筛选值。
	// 合并成局部副本而不是改调用方的 Props —— 同一组件可能在一次编译里被多处使用。
	if merged := p.withArchiveFilter(ctx); merged != nil {
		p = merged
	}
	filter := collectionFilter(p)
	page, pageSize := EffectivePage(p), EffectivePageSize(p)
	// 审计 PERF-019：**第 2 页起改为按页取数**（把 offset 下推到 SQL），而不是在「一次取回的
	// 集合源上限条」里切内存 —— 后者让超出上限的数据永远翻不到：翻到第 N 页拿到的仍是前 100
	// 条里的那一段，再往后就是空页，而分页控件还在。
	//
	// **第 1 页刻意保持原路径**：构建期只走这一支（发布产物恒定 page=1），换路径会改产物字节。
	// 首屏本来就只渲染第一页，两种取法在结果上等价，但字节等价只有原路径能保证。
	pagedFetch := pageSize > 0 && page > 1
	var items []map[string]any
	// total > 0 表示分页源给得出总量（用于判断还有没有下一页）；0 表示未知，按已取回条数判。
	total := 0
	var err error
	if pagedFetch {
		items, total, err = resolveProductsPage(ctx, source, filter, (page-1)*pageSize, pageSize)
	} else {
		items, err = resolveProducts(ctx, source, filter)
	}
	if err != nil {
		return View{}, fmt.Errorf("节点 %s: 商品集合解析失败: %w", node.ID, err)
	}
	sortItems(items, effectiveOrder(p))
	fetched := len(items)
	if !pagedFetch {
		if limit := effectiveLimit(p); len(items) > limit {
			items = items[:limit]
		}
		// 分页：先截到「本页末」，再取本页那段。顺序不能反 ——
		// 反了的话第 2 页会拿到「全部条目里的第 pageSize+1 条开始」，但总数判断却是截断后的，
		// 于是最后一页之后还会多出一页空列表。
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
		// 先落中文兜底：ApplyI18n 会在 BuildView 之后按语言覆盖（未接入 i18n 时就是这个值）。
		Labels: defaultListLabels(),
	}
	// 翻页可达性按**已取回条数**判断：不够就说明这一页之后没有更多了（不猜未取回的部分）。
	if pageSize > 0 {
		view.HasPrev = page > 1
		// 分页源给得出总量时用它判断（本页满不满意都不能说明还有没有下一页）；
		// 退化路径（无分页能力）仍按已取回条数判 —— 那是它唯一知道的信息。
		if total > 0 {
			view.HasNext = page*pageSize < total
		} else {
			view.HasNext = page*pageSize < fetched
		}
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
		view.Cards = append(view.Cards, cardViewOf(p, item, ctx.ResolveSiteLink))
	}

	view.FragmentQuery = lc.instanceQuery
	view.PushQuery = lc.pushQuery
	// 槽位解析是零成本的 map 读：没有条目就是「这个站还没指定商品列表页」，
	// 与「槽位绑了但那页没发布」同一条路（page 侧只返回已发布的绑定）。
	if href := strings.TrimSpace(ctx.SitePage(core.SiteSlotShop)); href != "" {
		view.HasListPageLink, view.ListPageLinkHref = true, href
		view.ListPageLinkText = effectiveListPageLinkText(p)
	}
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
	buildPriceSection(p, lc, &view)
	buildRatingSection(p, lc, &view)
	return view, nil
}

// cardViewOf 把一条集合项映射成卡片视图（逐槽位取值，空值不输出节点）。
//
// siteLink 站内链接本地化器（审计 I18N-015，可空）：LinkPrefix 是作者填的站内逻辑路径。
func cardViewOf(p *Props, item map[string]any, siteLink func(string) string) CardView {
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
			// 只本地化作者填的**前缀**，CMS 字段值原样参与拼接（见 productcard.LocalizePrefix）。
			view.Href = productcard.CardHref(productcard.LocalizePrefix(p.LinkPrefix, siteLink), value)
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

// itemPrice 取集合项的最低启用变体价（issue #28：集合源给的 minPrice 数值字段）。
//
// 缺失表示「没有启用变体」——与 0 元严格区分，排序时排到最后。
func itemPrice(item map[string]any) (float64, bool) {
	return source.ItemFloat(item, source.ItemFieldMinPrice)
}

// itemRating 取集合项的**数值**评分（issue #29 的 ratingValue）。
//
// 缺失表示「尚无评分」——与 0 分严格区分，排序时排到最后。
func itemRating(item map[string]any) (float64, bool) {
	return source.ItemFloat(item, source.ItemFieldRatingValue)
}

// sortItems 按 props 声明的口径重排（默认保持集合源的确定性序：排序号 → 创建时间 → id）。
//
// 时间解析失败的行按零值参与比较（排到最后）而不是丢弃：缺 createdAt 不该让商品消失。
func sortItems(items []map[string]any, order string) {
	if order == OrderDefault || len(items) < 2 {
		return
	}
	if order == OrderRatingDesc {
		// 评分排序（issue #29）：无评分（ratingValue 缺失）的排最后 —— 与价格同一口径，
		// 「还没人评过」不是「0 分」。
		sort.SliceStable(items, func(i, j int) bool {
			a, aok := itemRating(items[i])
			b, bok := itemRating(items[j])
			if aok != bok {
				return aok
			}
			if !aok {
				return false
			}
			return a > b
		})
		return
	}
	if order == OrderPriceAsc || order == OrderPriceDesc {
		// 价格排序（issue #28）：按**最低启用变体价**排。没有启用变体（minPrice 缺失）的排最后，
		// 不按 0 元参与比较 —— 「没有可售规格」不是「免费」。
		sort.SliceStable(items, func(i, j int) bool {
			a, aok := itemPrice(items[i])
			b, bok := itemPrice(items[j])
			if aok != bok {
				return aok
			}
			if !aok {
				return false
			}
			if order == OrderPriceAsc {
				return a < b
			}
			return a > b
		})
		return
	}
	createdAt := func(item map[string]any) time.Time {
		t, _ := source.ItemTime(item, source.ItemFieldCreatedAt)
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

// resolveProducts 取商品集合数据（issue #35）。
//
// 优先用**商品数据源** —— 装配期注入的受限接口，只有读集合 / 元数据 / 可筛值，
// 写方法不在它上面；未注入时回退到按名路由（纯组件单测路径 + 渐进切换期间的旧路径）。
func resolveProducts(ctx *core.RenderContext, source string, filter map[string]string) ([]map[string]any, error) {
	if ctx.Product != nil {
		return ctx.Product.ResolveCollection(ctx.Context, source, filter)
	}
	return ctx.Collection.ResolveCollection(ctx.Context, source, filter)
}

// resolveProductsPage 按页取数（审计 PERF-019）：集合源支持分页能力时把 offset 交给 SQL。
//
// 两条数据源都要探测：ctx.Product 是商品数据源（受限契约），ctx.Collection 是集合注册表 ——
// 两者都可能实现 CollectionPager，也可能都不实现（纯组件单测路径）。
// 不支持时不报错，退回「取一批再由调用方切片」：能力缺失只该让翻页退化成它本来的样子，
// 不该让整块列表渲染不出来。
//
// 第二个返回值是总量：> 0 时调用方用它判断还有没有下一页；0 表示源给不出（退化了）。
func resolveProductsPage(ctx *core.RenderContext, source string, filter map[string]string, offset, limit int) ([]map[string]any, int, error) {
	q := core.CollectionQuery{Filter: filter, Offset: offset, Limit: limit}
	if ctx.Product != nil {
		if pager, ok := ctx.Product.(core.CollectionPager); ok {
			page, err := pager.ResolveCollectionPage(ctx.Context, source, q)
			if err != nil {
				return nil, 0, err
			}
			total := page.Total
			if total < 0 {
				total = 0
			}
			return page.Items, total, nil
		}
		items, err := ctx.Product.ResolveCollection(ctx.Context, source, filter)
		return items, 0, err
	}
	if pager, ok := ctx.Collection.(core.CollectionPager); ok {
		page, err := pager.ResolveCollectionPage(ctx.Context, source, q)
		if err != nil {
			return nil, 0, err
		}
		total := page.Total
		if total < 0 {
			total = 0
		}
		return page.Items, total, nil
	}
	items, err := ctx.Collection.ResolveCollection(ctx.Context, source, filter)
	return items, 0, err
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：商品列表的容器 div 恒带
// 片段属性（product_list.jet 第一行）—— 包括空态：容器必须先渲染出来，HTMX 就位后才能按
// URL 上的查询参数把结果 load 进来。这也是「列表页首屏零数据查询」得以成立的地方。
func (v View) DeclareFeatures() (attrs, classes []string) {
	return []string{"hx-get", "hx-vals", "hx-trigger", "hx-target", "hx-swap"}, nil
}
