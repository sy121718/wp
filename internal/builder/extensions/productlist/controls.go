package productlist

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
)

func buildFilterSections(p *Props, ctx *core.RenderContext, lc linkContext, view *View) []FilterSection {
	if p == nil {
		return nil
	}
	wanted := map[string]bool{}
	for _, name := range splitList(p.Filters) {
		wanted[name] = true
	}
	if len(wanted) == 0 {
		return nil
	}
	options := view.FilterOptions
	sections := make([]FilterSection, 0, 4)

	if wanted["categories"] && len(options.Categories) > 0 {
		view.ShowCategories = true

		selected := selectedIDs(p.FilterCategoryIDs, p.FilterCategoryID)
		childrenOf := childrenIndex(options.Categories)

		multi := strings.TrimSpace(p.CategoryMulti) != "off"
		section := FilterSection{Title: "分类", Kind: "categories", Render: renderTree, CaretIcon: p.CaretIcon, Multi: multi}
		for _, item := range options.Categories {
			override := url.Values{}

			if multi {
				override.Set(filterKeyCategoryIDs, strings.Join(cascadeSelection(item.ID, selected, childrenOf), ","))
			} else if selected[item.ID] {
				override.Set(filterKeyCategoryIDs, "")
			} else {
				override.Set(filterKeyCategoryIDs, strings.Join(cascadeSelection(item.ID, map[string]bool{}, childrenOf), ","))
			}

			override.Set(filterKeyCategoryID, "")
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		byID := make(map[string]ControlOption, len(section.Options))
		for i, item := range options.Categories {
			byID[item.ID] = section.Options[i]
		}
		section.Tree = buildFilterTree(options.Categories, selected, childrenOf, byID, p.CaretIcon, multi)
		sections = append(sections, section)
	}

	if wanted["brands"] && len(options.Brands) > 0 {
		view.ShowBrands = true
		selected := selectedIDs(p.FilterBrandIDs, p.FilterBrandID)
		section := FilterSection{Title: "品牌", Kind: "brands", Render: renderButtons}
		for _, item := range options.Brands {
			override := url.Values{}
			override.Set(filterKeyBrandIDs, strings.Join(toggleID(selected, item.ID), ","))
			override.Set(filterKeyBrandID, "")
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		sections = append(sections, section)
	}

	if wanted["tags"] && len(options.Tags) > 0 {
		view.ShowTags = true
		selected := selectedIDs(p.FilterTagIDs, p.FilterTagID)
		section := FilterSection{Title: "标签", Kind: "tags", Render: renderButtons}
		for _, item := range options.Tags {
			override := url.Values{}
			override.Set(filterKeyTagIDs, strings.Join(toggleID(selected, item.ID), ","))
			override.Set(filterKeyTagID, "")
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		sections = append(sections, section)
	}

	if wanted["attributes"] {

		current := parseOptionPairs(p.FilterOptions)
		for _, group := range options.Attributes {
			if len(group.Values) == 0 {
				continue
			}
			view.ShowAttributes = true
			selected := map[string]bool{}
			for _, key := range current[group.Key] {
				selected[key] = true
			}
			section := FilterSection{Title: group.Name, Kind: "attributes", Render: renderButtons}
			for _, value := range group.Values {
				override := url.Values{}
				override.Set("option."+group.Key, strings.Join(toggleKey(selected, value.Key), ","))
				override.Set("page", "")
				section.Options = append(section.Options, controlOption(lc, value.Name, selected[value.Key], override))
			}
			sections = append(sections, section)
		}
	}
	return sections
}

type ControlOption struct {
	Label string

	Active bool

	Disabled bool

	DisabledHint string

	Href string

	ActionQuery string

	PushURL string
}

type FilterSection struct {
	Title string

	Kind string

	Render string

	Options []ControlOption

	Tree []TreeItem

	CaretIcon string

	Multi bool
}

type TreeItem struct {
	Option ControlOption

	ID string

	Depth int

	State string

	OpenTags string

	MidTags string

	CloseTags string
}

const (
	stateAll = "all"

	statePartial = "partial"

	stateNone = "none"
)

func buildFilterTree(options []core.CollectionFilterChoice, selected map[string]bool, childrenOf map[string][]string, opts map[string]ControlOption, caretIcon string, multi bool) []TreeItem {
	if len(options) == 0 {
		return nil
	}
	byID := make(map[string]core.CollectionFilterChoice, len(options))
	for _, opt := range options {
		if opt.ID != "" {
			byID[opt.ID] = opt
		}
	}
	children := map[string][]core.CollectionFilterChoice{}
	roots := make([]core.CollectionFilterChoice, 0, len(options))
	for _, opt := range options {

		_, parentKnown := byID[opt.ParentID]
		if opt.ParentID == "" || opt.ID == opt.ParentID || !parentKnown {
			roots = append(roots, opt)
			continue
		}
		children[opt.ParentID] = append(children[opt.ParentID], opt)
	}
	out := make([]TreeItem, 0, len(options))

	var walk func(opt core.CollectionFilterChoice, depth int, seen map[string]bool) (allSel, anySel bool)
	walk = func(opt core.CollectionFilterChoice, depth int, seen map[string]bool) (allSel, anySel bool) {
		idx := len(out)
		out = append(out, TreeItem{Option: opts[opt.ID], ID: opt.ID, Depth: depth})
		kids, hasKids := children[opt.ID]

		if depth >= maxFilterTreeDepth {
			hasKids = false
		}
		childAll, childAny := true, false
		if hasKids {
			next := make(map[string]bool, len(seen)+1)
			for k := range seen {
				next[k] = true
			}
			next[opt.ID] = true
			for _, child := range kids {
				if next[child.ID] {
					continue
				}
				a, y := walk(child, depth+1, next)
				if !a {
					childAll = false
				}
				if y {
					childAny = true
				}
			}
		}

		state := stateNone
		if !multi {

			if selected[opt.ID] {
				state = stateAll
			}
		} else if selected[opt.ID] && childAll {
			state = stateAll
			allSel, anySel = true, true
		} else if selected[opt.ID] || childAny {
			state = statePartial
			anySel = true
		}
		item := &out[idx]
		item.State = state
		if hasKids {
			openAttr := ""
			if state != stateNone {
				openAttr = " open"
			}
			item.OpenTags = fmt.Sprintf(
				"<li class=\"sky-product-list-node\" data-depth=\"%d\" data-state=\"%s\">"+
					"<details class=\"sky-product-list-node-group\"%s>"+
					"<summary class=\"sky-product-list-node-caret-row\">",
				depth, state, openAttr)

			item.MidTags = "</summary><ul class=\"sky-product-list-node-children\">"

			item.CloseTags = ""

			iconName := "chevron-right"
			if caretIcon != "" {
				iconName = caretIcon
			}
			if iconName != "none" {
				if svg, ok := core.IconSVGClass(iconName, "sky-product-list-node-caret"); ok {
					item.OpenTags += svg
				}
			}
		} else {
			item.OpenTags = fmt.Sprintf("<li class=\"sky-product-list-node\" data-depth=\"%d\" data-state=\"%s\">", depth, state)
			item.CloseTags = "</li>"
		}
		return allSel, anySel
	}
	for _, root := range roots {
		walk(root, 0, map[string]bool{})
	}

	for i := range out {
		var drop int
		if i+1 < len(out) {
			if drop = out[i].Depth - out[i+1].Depth; drop < 0 {
				drop = 0
			}
		} else {
			drop = out[i].Depth + 1
		}
		if drop > 0 {
			out[i].CloseTags += strings.Repeat("</ul></details></li>", drop)
		}
	}
	return out
}

const (
	renderTree = "tree"

	renderButtons = "buttons"

	renderCheckbox    = "checkbox"
	renderSelect      = "select"
	renderMultiSelect = "multi-select"
)

const maxFilterTreeDepth = 5

func cascadeSelection(clicked string, selected map[string]bool, childrenOf map[string][]string) []string {
	next := make(map[string]bool, len(selected)+4)
	for id := range selected {
		next[id] = true
	}
	if next[clicked] {
		delete(next, clicked)
		for _, id := range collectSubtree(clicked, childrenOf) {
			delete(next, id)
		}
	} else {
		next[clicked] = true
		for _, id := range collectSubtree(clicked, childrenOf) {
			next[id] = true
		}
	}
	out := make([]string, 0, len(next))
	for id := range next {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func collectSubtree(root string, childrenOf map[string][]string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{root: true}
	var walk func(id string)
	walk = func(id string) {
		for _, child := range childrenOf[id] {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			walk(child)
		}
	}
	walk(root)
	return out
}

func childrenIndex(options []core.CollectionFilterChoice) map[string][]string {
	known := make(map[string]bool, len(options))
	for _, opt := range options {
		known[opt.ID] = true
	}
	out := make(map[string][]string, len(options))
	for _, opt := range options {

		if opt.ParentID == "" || opt.ID == opt.ParentID || !known[opt.ParentID] {
			continue
		}
		out[opt.ParentID] = append(out[opt.ParentID], opt.ID)
	}
	return out
}

func subtreeFullySelected(id string, selected map[string]bool, childrenOf map[string][]string) bool {
	sub := collectSubtree(id, childrenOf)
	if len(sub) == 0 {
		return selected[id]
	}
	for _, child := range sub {
		if !selected[child] {
			return false
		}
	}
	return true
}

func subtreeAnySelected(id string, selected map[string]bool, childrenOf map[string][]string) bool {
	for _, child := range collectSubtree(id, childrenOf) {
		if selected[child] {
			return true
		}
	}
	return false
}

func selectedIDs(multi, single string) map[string]bool {
	out := map[string]bool{}
	for _, id := range splitList(multi) {
		out[id] = true
	}
	if len(out) == 0 {
		if id := strings.TrimSpace(single); id != "" {
			out[id] = true
		}
	}
	return out
}

func toggleID(selected map[string]bool, id string) []string {
	next := make([]string, 0, len(selected)+1)
	for cur := range selected {
		if cur != id {
			next = append(next, cur)
		}
	}
	if !selected[id] {
		next = append(next, id)
	}
	sort.Strings(next)
	return next
}

func toggleKey(selected map[string]bool, key string) []string {
	return toggleID(selected, key)
}

func buildSortOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := effectiveOrder(p)
	type item struct{ key, label string }
	items := []item{
		{OrderDefault, "默认排序"},
		{OrderNewest, "最新上架"},
		{OrderOldest, "最早上架"},

		{OrderRatingDesc, "评分最高"},

		{OrderPriceAsc, "价格从低到高"},
		{OrderPriceDesc, "价格从高到低"},
	}
	out := make([]ControlOption, 0, len(items)+1)
	for _, it := range items {
		override := url.Values{}
		if it.key == OrderDefault {
			override.Set("orderBy", "")
		} else {
			override.Set("orderBy", it.key)
		}
		override.Set("page", "")
		out = append(out, controlOption(lc, it.label, current == it.key, override))
	}

	out = append(out, ControlOption{
		Label: "按热度", Active: false, Disabled: true,
		DisabledHint: "需要销量或浏览数据（订单域落地后接入）",
	})
	view.ShowSort = true
	return out
}

func buildPageSizeOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := EffectivePageSize(p)
	sizes := []int{20, 40, 60}
	out := make([]ControlOption, 0, len(sizes))
	for _, size := range sizes {
		override := url.Values{}
		override.Set("pageSize", strconv.Itoa(size))
		override.Set("page", "")
		out = append(out, controlOption(lc, strconv.Itoa(size)+" 条", current == size, override))
	}
	view.ShowPageSize = true
	return out
}

func buildColumnOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := effectiveColumns(p)
	type item struct{ key, label string }
	items := []item{
		{LayoutList, "列表"}, {"2", "2 列"}, {"3", "3 列"}, {"4", "4 列"},
	}
	out := make([]ControlOption, 0, len(items))
	for _, it := range items {
		override := url.Values{}
		if it.key == LayoutList {
			override.Set("layout", LayoutList)
		} else {
			override.Set("layout", LayoutGrid)
			override.Set("columns", it.key)
		}
		out = append(out, controlOption(lc, it.label, current == it.key, override))
	}
	view.ShowColumns = true
	return out
}

func buildOnSaleOptions(p *Props, lc linkContext, view *View) []ControlOption {
	active := strings.TrimSpace(p.OnlyOnSale) == "on"
	override := url.Values{}
	if active {
		override.Set("onSale", "")
	} else {
		override.Set("onSale", "true")
	}
	override.Set("page", "")
	view.ShowOnSale = true
	return []ControlOption{controlOption(lc, "只看在售", active, override)}
}

func buildPriceSection(p *Props, lc linkContext, view *View) {
	ranges := splitList(p.PriceRanges)
	slider := priceSliderVisible(p, ranges)
	currentMin, currentMax := currentPriceFilter(p)

	options := make([]ControlOption, 0, len(ranges))
	for _, raw := range ranges {
		rng, label, err := ParsePriceRange(raw)
		if err != nil {
			continue
		}
		active := priceRangeActive(rng, currentMin, currentMax)
		override := url.Values{}
		if active {

			override.Set("minPrice", "")
			override.Set("maxPrice", "")
		} else {
			override.Set("minPrice", trimNumber(rng.Min))
			if rng.Max != nil {
				override.Set("maxPrice", trimNumber(*rng.Max))
			} else {
				override.Set("maxPrice", "")
			}
		}
		override.Set("page", "")
		options = append(options, controlOption(lc, label, active, override))
	}
	if len(options) == 0 && !slider {
		return
	}
	view.HasPriceSection = true
	view.PriceOptions = options
	if !slider {
		return
	}

	boundMin, boundMax, err := EffectivePriceBounds(p)
	if err != nil {
		boundMin, boundMax = 0, 1000
	}
	view.ShowPriceSlider = true
	view.PriceBoundMin = trimNumber(boundMin)
	view.PriceBoundMax = trimNumber(boundMax)

	view.PriceFromValue = trimNumber(boundMin)
	view.PriceToValue = trimNumber(boundMax)
	if currentMin != nil {
		view.PriceFromValue = trimNumber(*currentMin)
	}
	if currentMax != nil {
		view.PriceToValue = trimNumber(*currentMax)
	}

	view.PriceFormAction = lc.pushURL(url.Values{})
}

func priceSliderVisible(p *Props, ranges []string) bool {
	if !PriceSliderEnabled(p) {
		return false
	}
	if strings.TrimSpace(p.PriceSlider) == "on" {
		return true
	}
	return len(ranges) > 0
}

func currentPriceFilter(p *Props) (min, max *float64) {
	if p == nil {
		return nil, nil
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(p.FilterMinPrice), 64); err == nil && strings.TrimSpace(p.FilterMinPrice) != "" {
		min = &v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(p.FilterMaxPrice), 64); err == nil && strings.TrimSpace(p.FilterMaxPrice) != "" {
		max = &v
	}
	return min, max
}

func priceRangeActive(rng PriceRange, min, max *float64) bool {
	if min == nil || *min != rng.Min {
		return false
	}
	if rng.Max == nil {
		return max == nil
	}
	return max != nil && *max == *rng.Max
}

func trimNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func buildRatingSection(p *Props, lc linkContext, view *View) {
	scores := splitList(p.RatingOptions)
	if len(scores) == 0 {
		return
	}
	current := strings.TrimSpace(p.FilterMinRating)
	options := make([]ControlOption, 0, len(scores))
	for _, raw := range scores {
		score := strings.TrimSpace(raw)
		active := current != "" && current == score
		override := url.Values{}
		if active {
			override.Set("minRating", "")
		} else {
			override.Set("minRating", score)
		}
		override.Set("page", "")
		options = append(options, controlOption(lc, "≥ "+score+" 星", active, override))
	}
	view.HasRatingSection = true
	view.RatingOptions = options
}

func controlOption(lc linkContext, label string, active bool, override url.Values) ControlOption {
	return ControlOption{
		Label:  label,
		Active: active,

		Href:        lc.fallbackHref(override),
		ActionQuery: override.Encode(),
		PushURL:     lc.pushURL(override),
	}
}

func pageOverride(page int) url.Values {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	return v
}

func pageItems(lc linkContext, page, totalPages int) ([]ControlOption, []bool) {
	if totalPages <= 1 {
		return nil, nil
	}

	set := map[int]bool{1: true, totalPages: true}
	for p := page - pageWindowSpan; p <= page+pageWindowSpan; p++ {
		if p >= 1 && p <= totalPages {
			set[p] = true
		}
	}
	pages := make([]int, 0, len(set))
	for p := range set {
		pages = append(pages, p)
	}
	sort.Ints(pages)
	items := make([]ControlOption, 0, len(pages))
	gaps := make([]bool, 0, len(pages))
	prev := 0
	for _, p := range pages {

		gaps = append(gaps, prev != 0 && p-prev > 1)
		items = append(items, controlOption(lc, strconv.Itoa(p), p == page, pageOverride(p)))
		prev = p
	}
	return items, gaps
}

const pageWindowSpan = 2

func parseOptionPairs(raw string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, pair := range strings.Split(raw, ",") {
		key, values, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		for _, value := range strings.Split(values, ",") {
			value = strings.TrimSpace(value)
			if value == "" || seen[key][value] {
				continue
			}
			seen[key][value] = true
			out[key] = append(out[key], value)
		}
	}
	return out
}
