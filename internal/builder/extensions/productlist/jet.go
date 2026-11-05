package productlist

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	productcard "go_wp/internal/builder/extensions/productcard"
	"go_wp/pkg/builder/core"

	"go_wp/pkg/builder/source"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

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

	HasDiscount     bool
	DiscountPercent string

	Tags []string
}

type View struct {
	Cards []CardView

	Empty     bool
	EmptyText string

	List bool

	Page     int
	PageSize int

	HasPrev bool
	HasNext bool

	FetchedTotal int

	FilterOptions core.CollectionFilterOptions

	ShowCategories bool
	ShowBrands     bool
	ShowTags       bool
	ShowAttributes bool
	ShowSort       bool
	ShowPageSize   bool
	ShowColumns    bool
	ShowOnSale     bool

	HasRatingSection bool
	RatingOptions    []ControlOption

	HasPriceSection bool
	PriceOptions    []ControlOption
	ShowPriceSlider bool

	PriceBoundMin string
	PriceBoundMax string

	PriceFromValue string
	PriceToValue   string

	PriceFormAction string

	FilterSections  []FilterSection
	SortOptions     []ControlOption
	PageSizeOptions []ControlOption
	ColumnOptions   []ControlOption
	OnSaleOptions   []ControlOption

	PrevPage int
	NextPage int

	HasPager bool

	Labels ListLabels

	PageText string

	PrevLink ControlOption
	NextLink ControlOption

	TotalPages int

	PageItems []ControlOption

	Gaps []bool

	DataQuery         string
	DataURL           string
	Currency          string
	TitleTag          string
	ImageField        string
	ImageAltField     string
	TitleField        string
	PriceField        string
	ComparePriceField string
	TagsField         string
	CategorySingle    bool

	PushQuery string

	HasListPageLink  bool
	ListPageLinkHref string
	ListPageLinkText string
	ListPageNewTab   bool
}

func collectionFilterOptionsProvider(ctx *core.RenderContext) (core.CollectionFilterOptionsProvider, bool) {
	if ctx == nil {
		return nil, false
	}
	if ctx.Product != nil {
		if provider, ok := ctx.Product.(core.CollectionFilterOptionsProvider); ok {
			return provider, true
		}
	}
	if ctx.Collection != nil {
		if provider, ok := ctx.Collection.(core.CollectionFilterOptionsProvider); ok {
			return provider, true
		}
	}
	return nil, false
}

func toolbarWanted(p *Props, name string) bool {
	for _, item := range splitList(p.Toolbar) {
		if item == name {
			return true
		}
	}
	return false
}

func IsCollection(_ *Props) bool { return true }

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
	case "category", "product_category":
		clone.FilterCategoryID = entityID
	case "brand", "product_brand":
		clone.FilterBrandID = entityID
	case "tag", "product_tag":
		clone.FilterTagIDs = entityID
	default:
		return nil
	}
	return &clone
}

func BuildView(node *core.Node, p *Props, ctx *core.RenderContext) (View, error) {
	source := effectiveSource(p)
	if source == "" {

		return View{Empty: true, EmptyText: effectiveEmptyText(p), List: effectiveLayout(p) == LayoutList},
			nil
	}
	if ctx == nil || ctx.Collection == nil {
		return View{}, fmt.Errorf("节点 %s: 编译上下文缺少集合解析器（无法解析商品集合 %s）", node.ID, source)
	}

	if merged := p.withArchiveFilter(ctx); merged != nil {
		p = merged
	}
	filter := collectionFilter(p)
	page, pageSize := EffectivePage(p), EffectivePageSize(p)

	pagedFetch := pageSize > 0 && page > 1
	var items []map[string]any

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

	lc := newLinkContext(node.ID, p, ctx)

	view := View{
		Cards:        make([]CardView, 0, len(items)),
		Empty:        len(items) == 0 && page <= 1,
		EmptyText:    effectiveEmptyText(p),
		List:         effectiveLayout(p) == LayoutList,
		Page:         page,
		PageSize:     pageSize,
		FetchedTotal: fetched,

		Labels: defaultListLabels(),
	}

	if pageSize > 0 {
		view.HasPrev = page > 1

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

		if total > 0 {
			view.TotalPages = (total + pageSize - 1) / pageSize
			view.PageItems, view.Gaps = pageItems(lc, page, view.TotalPages)
		}
	}
	for _, item := range items {
		view.Cards = append(view.Cards, cardViewOf(p, item, ctx.ResolveSiteLink))
	}

	view.DataQuery = lc.instanceQuery
	view.DataURL = catalogPath + "?" + lc.instanceQuery
	view.Currency, view.TitleTag = effectiveCurrency(p), effectiveTitleTag(p)
	view.CategorySingle = p.CategoryMulti == "off"
	view.ImageField, view.ImageAltField, view.TitleField = p.ImageField, p.ImageAltField, p.TitleField
	view.PriceField, view.ComparePriceField, view.TagsField = p.PriceField, p.ComparePriceField, p.TagsField
	view.PushQuery = lc.pushQuery
	if _, configured := node.Actions["list"]; configured {
		action, err := ctx.ResolveAction(node, "list")
		if err != nil {
			return View{}, err
		}
		view.HasListPageLink, view.ListPageLinkHref = action.URL != "", action.URL
		view.ListPageLinkText = effectiveListPageLinkText(p)
		view.ListPageNewTab = action.NewTab
	}

	if provider, ok := collectionFilterOptionsProvider(ctx); ok && len(splitList(p.Filters)) > 0 {
		if options, oerr := provider.CollectionFilterOptions(ctx.Context, source, ctx.ProjectID); oerr == nil {
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

func cardViewOf(p *Props, item map[string]any, siteLink func(string) string) CardView {
	view := CardView{TitleTag: effectiveTitleTag(p)}
	var rawImageAlt, rawTitle string

	var rawPrice, rawCompare string
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
			rawPrice = value
			view.HasPrice, view.Price = true, productcard.FormatPrice(effectiveCurrency(p), value)
		case slotComparePrice:
			rawCompare = value
			view.HasComparePrice, view.ComparePrice = true, productcard.FormatPrice(effectiveCurrency(p), value)
		case slotTags:
			view.Tags = productcard.ParseTagNames(value)
		case slotLink:

			view.Href = productcard.CardHref(productcard.LocalizePrefix(p.LinkPrefix, siteLink), value)
		}
	}

	if view.HasImage {
		view.ImageAlt = rawImageAlt
		if view.ImageAlt == "" {
			view.ImageAlt = rawTitle
		}
	}

	if percent := productcard.DiscountPercent(rawPrice, rawCompare); percent > 0 {
		view.HasDiscount, view.DiscountPercent = true, "-"+strconv.Itoa(percent)+"%"
	}
	return view
}

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

func itemPrice(item map[string]any) (float64, bool) {
	return source.ItemFloat(item, source.ItemFieldMinPrice)
}

func itemRating(item map[string]any) (float64, bool) {
	return source.ItemFloat(item, source.ItemFieldRatingValue)
}

func sortItems(items []map[string]any, order string) {
	if order == OrderDefault || len(items) < 2 {
		return
	}
	if order == OrderRatingDesc {

		slices.SortStableFunc(items, func(a, b map[string]any) int {
			ra, aok := itemRating(a)
			rb, bok := itemRating(b)
			if aok != bok {
				if aok {
					return -1
				}
				return 1
			}
			if !aok {
				return 0
			}
			return cmp.Compare(rb, ra)
		})
		return
	}
	if order == OrderPriceAsc || order == OrderPriceDesc {

		slices.SortStableFunc(items, func(a, b map[string]any) int {
			pa, aok := itemPrice(a)
			pb, bok := itemPrice(b)
			if aok != bok {
				if aok {
					return -1
				}
				return 1
			}
			if !aok {
				return 0
			}
			if order == OrderPriceAsc {
				return cmp.Compare(pa, pb)
			}
			return cmp.Compare(pb, pa)
		})
		return
	}
	createdAt := func(item map[string]any) time.Time {
		t, _ := source.ItemTime(item, source.ItemFieldCreatedAt)
		return t
	}
	slices.SortStableFunc(items, func(a, b map[string]any) int {
		ta, tb := createdAt(a), createdAt(b)
		if order == OrderNewest {
			return tb.Compare(ta)
		}
		return ta.Compare(tb)
	})
}

func resolveProducts(ctx *core.RenderContext, source string, filter map[string]string) ([]map[string]any, error) {
	if ctx.Product != nil {
		return ctx.Product.ResolveCollection(ctx.Context, source, filter)
	}
	return ctx.Collection.ResolveCollection(ctx.Context, source, filter)
}

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

func (v View) DeclareFeatures() (attrs, classes []string) {
	return []string{"data-site-read", "data-site-view", "data-site-catalog"}, nil
}
