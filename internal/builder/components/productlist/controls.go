// controls.go — 筛选栏 / 工具条 / 分页的控件构造（issue #27）。
//
// 所有控件都是**链接**（<a>）而不是 select/checkbox：
//
//	· 无 JS 时它们是可点的普通链接（整页跳转，URL 语义参数照旧生效）；
//	· 有 HTMX 时同一批链接带 hx-get / hx-push-url，变成局部刷新；
//	· 键盘可达与读屏语义都是链接原生带来的，不需要额外 ARIA 体操。
//
// 每个控件同时给出三个 URL：href（降级整页跳转）、FragmentGet（片段请求）、PushURL（地址栏）。
// 三者的分工见 links.go。
package productlist

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// ControlOption 一个可点的交互项。
type ControlOption struct {
	Label string
	// Active 当前选中（渲染成高亮态 + aria-current）。
	Active bool
	// Disabled 不可用（如 popularity 排序：系统里没有热度数据）。
	Disabled bool
	// DisabledHint 不可用原因（title 属性，鼠标悬停可见）。
	DisabledHint string
	// Href 降级链接（只带语义参数；无 JS 时整页跳转）。
	Href string
	// FragmentGet 片段请求 URL（实例配置 + 当前语义参数 + 本次变化）。
	FragmentGet string
	// PushURL 地址栏要推的查询串（只带语义参数）。
	PushURL string
}

// FilterSection 筛选栏的一块（分类 / 品牌 / 标签 / 某个属性组）。
type FilterSection struct {
	// Title 块标题（分类 / 品牌 / 标签 / 属性组名）。
	Title string
	// Kind 块的种类（categories / brands / tags / attributes）—— 模板据此加类名。
	Kind string
	// Options 可选值。
	Options []ControlOption
}

// buildFilterSections 构造筛选栏（只包含作者勾选、且集合源确实给了值的块）。
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
		selected := strings.TrimSpace(p.FilterCategoryID)
		section := FilterSection{Title: "分类", Kind: "categories"}
		for _, item := range options.Categories {
			override := url.Values{}
			if item.ID == selected {
				override.Set("categoryId", "") // 再点一次 = 取消该筛选
			} else {
				override.Set("categoryId", item.ID)
			}
			override.Set("page", "") // 换筛选回到第 1 页，否则会落在越界页
			section.Options = append(section.Options, controlOption(lc, item.Name, item.ID == selected, override))
		}
		sections = append(sections, section)
	}

	if wanted["brands"] && len(options.Brands) > 0 {
		view.ShowBrands = true
		selected := strings.TrimSpace(p.FilterBrandID)
		section := FilterSection{Title: "品牌", Kind: "brands"}
		for _, item := range options.Brands {
			override := url.Values{}
			if item.ID == selected {
				override.Set("brandId", "")
			} else {
				override.Set("brandId", item.ID)
			}
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, item.ID == selected, override))
		}
		sections = append(sections, section)
	}

	if wanted["tags"] && len(options.Tags) > 0 {
		view.ShowTags = true
		selected := map[string]bool{}
		for _, id := range splitList(p.FilterTagIDs) {
			selected[id] = true
		}
		section := FilterSection{Title: "标签", Kind: "tags"}
		for _, item := range options.Tags {
			// 多标签：点一次加入、再点移除（当前选中集合只在本次渲染里算，不改动 props）。
			next := make([]string, 0, len(selected)+1)
			for id := range selected {
				if id != item.ID {
					next = append(next, id)
				}
			}
			if !selected[item.ID] {
				next = append(next, item.ID)
			}
			sort.Strings(next)
			override := url.Values{}
			override.Set("tagIds", strings.Join(next, ","))
			override.Set("page", "")
			section.Options = append(section.Options, controlOption(lc, item.Name, selected[item.ID], override))
		}
		sections = append(sections, section)
	}

	if wanted["attributes"] {
		// 属性选择是**每组单选**：点同组另一个值 = 换值，点已选值 = 取消该组。
		current := parseOptionPairs(p.FilterOptions)
		for _, group := range options.Attributes {
			if len(group.Values) == 0 {
				continue
			}
			view.ShowAttributes = true
			section := FilterSection{Title: group.Name, Kind: "attributes"}
			for _, value := range group.Values {
				override := url.Values{}
				selected := current[group.Key] == value.Key
				// 先清掉该组当前值，再按需写回（换值 / 取消都走这条路）。
				override.Set("option."+group.Key, "")
				if !selected {
					override.Set("option."+group.Key, value.Key)
				}
				override.Set("page", "")
				section.Options = append(section.Options, controlOption(lc, value.Name, selected, override))
			}
			sections = append(sections, section)
		}
	}
	return sections
}

// buildSortOptions 排序下拉的选项（价格排序在 #28；热度是禁用占位）。
func buildSortOptions(p *Props, lc linkContext, view *View) []ControlOption {
	current := effectiveOrder(p)
	type item struct{ key, label string }
	items := []item{
		{OrderDefault, "默认排序"},
		{OrderNewest, "最新上架"},
		{OrderOldest, "最早上架"},
		// 价格两条（issue #28）：按最低启用变体价升 / 降。
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
	// 热度：系统里没有销量 / 埋点数据，做成禁用占位而不是假装能排。
	out = append(out, ControlOption{
		Label: "按热度", Active: false, Disabled: true,
		DisabledHint: "需要销量或浏览数据（订单域落地后接入）",
	})
	view.ShowSort = true
	return out
}

// buildPageSizeOptions 每页条数选项。
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

// buildColumnOptions 网格密度选项（列表 / 2 / 3 / 4 列）。
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

// buildOnSaleOptions 「只看在售」开关（对应 #11 的 on_sale 自动标签判定：
// 存在启用变体且有划线价高于售价）。
//
// 做一个可点开关而不是复选框：复选框需要 JS 维护状态与提交，链接天然可点、可分享、
// 无 JS 时也照常工作（点击后 URL 带 onSale=true，整页刷新同样是筛过的结果）。
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

// buildPriceSection 价格块（issue #28）：预设档位 + 可拖动滑块。
//
// 显示规则（写死在这里，避免「配了档位却看不到块」这种猜谜）：
//
//	· PriceRanges 非空 → 显示价格块（含档位）；
//	· PriceSlider = on → 即使没有档位也显示（只想要滑块的情况）；
//	· PriceSlider = off → 不显示滑块（档位仍按上一条显示）。
func buildPriceSection(p *Props, lc linkContext, view *View) {
	ranges := splitList(p.PriceRanges)
	slider := priceSliderVisible(p, ranges)
	currentMin, currentMax := currentPriceFilter(p)

	options := make([]ControlOption, 0, len(ranges))
	for _, raw := range ranges {
		rng, label, err := ParsePriceRange(raw)
		if err != nil {
			continue // 形状在 validateExtra 已拦；这里静默跳过，不制造半个档位
		}
		active := priceRangeActive(rng, currentMin, currentMax)
		override := url.Values{}
		if active {
			// 再点一次 = 取消该档（与其它筛选同一交互约定）。
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
		override.Set("page", "") // 换价格回到第 1 页
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
		boundMin, boundMax = 0, 1000 // 同上：形状已在配置期拦下
	}
	view.ShowPriceSlider = true
	view.PriceBoundMin = trimNumber(boundMin)
	view.PriceBoundMax = trimNumber(boundMax)
	// 当前区间的两个端点：没设的端点回落到「滑块边界」，这样输入框里显示的总是个确切值
	// （空 value 的 number 输入框在浏览器里会显示成空，用户不知道范围是多少）。
	view.PriceFromValue = trimNumber(boundMin)
	view.PriceToValue = trimNumber(boundMax)
	if currentMin != nil {
		view.PriceFromValue = trimNumber(*currentMin)
	}
	if currentMax != nil {
		view.PriceToValue = trimNumber(*currentMax)
	}
	// 表单两条路：有 JS 走片段局部刷新（hx-get 带实例配置），没 JS 走原生 GET 到干净 URL
	// （action 只带语义参数，表单字段把 minPrice / maxPrice 附上去，冷启动补正接住结果）。
	view.PriceFragmentGet = lc.fragmentGet(url.Values{})
	view.PriceFormAction = lc.pushURL(url.Values{})
}

// priceSliderVisible 滑块是否渲染。
func priceSliderVisible(p *Props, ranges []string) bool {
	if !PriceSliderEnabled(p) {
		return false
	}
	if strings.TrimSpace(p.PriceSlider) == "on" {
		return true
	}
	return len(ranges) > 0
}

// currentPriceFilter 当前生效的价格区间（来自 props；非法值按「未设」处理）。
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

// priceRangeActive 当前区间是否正好等于这个档位（用于高亮）。
func priceRangeActive(rng PriceRange, min, max *float64) bool {
	if min == nil || *min != rng.Min {
		return false
	}
	if rng.Max == nil {
		return max == nil
	}
	return max != nil && *max == *rng.Max
}

// trimNumber 数字 → 最简字符串（100 而不是 100.000000；100.5 保留小数）。
func trimNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// controlOption 拼一个控件的三个 URL。
func controlOption(lc linkContext, label string, active bool, override url.Values) ControlOption {
	return ControlOption{
		Label:       label,
		Active:      active,
		Href:        lc.pushURL(override),
		FragmentGet: lc.fragmentGet(override),
		PushURL:     lc.pushURL(override),
	}
}

// pageOverride 翻页的语义参数覆盖（只改 page，其余筛选原样保留）。
func pageOverride(page int) url.Values {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	return v
}

// parseOptionPairs `key:value,key:value` → map（重复键后者胜，与下推口径一致）。
func parseOptionPairs(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}
