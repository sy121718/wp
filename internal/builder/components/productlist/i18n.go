package productlist

// i18n.go — 列表固定文案的多语言取词（审计 I18N-010）。
//
// 这些文案（筛选 / 价格 / 排序 / 每页 / 视图 / 在售 / 分页）此前硬编码在模板里，
// 会随产物一起烘进静态 HTML：站点切到英文后它们仍是中文，而且**发布后改不了**
//（改一句文案要改代码再重新构建）。
//
// 集中成一个结构体而不是散成十几个 View 字段：这些文案只有一处来源、一起填充、
// 一起在模板里用 —— 散开之后「哪些文案还没接多语言」就看不出来了。

import (
	"fmt"
	"strconv"
)

// 文案 key（sys_i18n）：site.component.productList.<语义>。
const (
	TextKeyFilters      = "site.component.productList.filters"
	TextKeyRating       = "site.component.productList.rating"
	TextKeyPrice        = "site.component.productList.price"
	TextKeyPriceMin     = "site.component.productList.priceMin"
	TextKeyPriceMinAria = "site.component.productList.priceMinAria"
	TextKeyPriceMax     = "site.component.productList.priceMax"
	TextKeyPriceMaxAria = "site.component.productList.priceMaxAria"
	TextKeyPriceApply   = "site.component.productList.priceApply"
	TextKeySort         = "site.component.productList.sort"
	TextKeyPageSize     = "site.component.productList.pageSize"
	TextKeyView         = "site.component.productList.view"
	TextKeyOnSale       = "site.component.productList.onSale"
	TextKeyPager        = "site.component.productList.pager"
	TextKeyPrev         = "site.component.productList.prev"
	TextKeyNext         = "site.component.productList.next"
	TextKeyPageCurrent  = "site.component.productList.pageCurrent"
)

// 中文兜底：取词函数为 nil（未接入 i18n）时用这些值，产物与接入前逐字一致。
const (
	fallbackFilters      = "筛选"
	fallbackRating       = "评分"
	fallbackPrice        = "价格"
	fallbackPriceMin     = "最低"
	fallbackPriceMinAria = "最低价"
	fallbackPriceMax     = "最高"
	fallbackPriceMaxAria = "最高价"
	fallbackPriceApply   = "应用价格"
	fallbackSort         = "排序"
	fallbackPageSize     = "每页"
	fallbackView         = "视图"
	fallbackOnSale       = "在售"
	fallbackPager        = "分页"
	fallbackPrev         = "上一页"
	fallbackNext         = "下一页"
	// fallbackPageCurrent 含计数占位。译文整串替换而不是拼词序 ——
	// 「第 N 页」在别的语言里不是「前缀 + 数字 + 后缀」的形状。
	//
	// 占位符用 %s 而不是 %d：词条侧的约定只允许 %s
	//（pkg/i18n.HasStringPlaceholdersOnly），译文里出现 %d 会被当成非法占位符。
	fallbackPageCurrent = "第 %s 页"
)

// ListLabels 列表的固定文案（构建期按语言回填）。
type ListLabels struct {
	Filters      string
	Rating       string
	Price        string
	PriceMin     string
	PriceMinAria string
	PriceMax     string
	PriceMaxAria string
	PriceApply   string
	Sort         string
	PageSize     string
	View         string
	OnSale       string
	Pager        string
	Prev         string
	Next         string
	PageCurrent  string
}

// defaultListLabels 中文兜底值（也是未接入 i18n 时的产物来源）。
func defaultListLabels() ListLabels {
	return ListLabels{
		Filters:      fallbackFilters,
		Rating:       fallbackRating,
		Price:        fallbackPrice,
		PriceMin:     fallbackPriceMin,
		PriceMinAria: fallbackPriceMinAria,
		PriceMax:     fallbackPriceMax,
		PriceMaxAria: fallbackPriceMaxAria,
		PriceApply:   fallbackPriceApply,
		Sort:         fallbackSort,
		PageSize:     fallbackPageSize,
		View:         fallbackView,
		OnSale:       fallbackOnSale,
		Pager:        fallbackPager,
		Prev:         fallbackPrev,
		Next:         fallbackNext,
		PageCurrent:  fallbackPageCurrent,
	}
}

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// text 为 nil 时直接落中文兜底（与未接入 i18n 的产物逐字一致）；
// PageText 在这里一次算好 —— 它带计数，模板做不了格式化。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Labels = defaultListLabels()
		v.PageText = fmt.Sprintf(v.Labels.PageCurrent, strconv.Itoa(v.Page))
		return
	}
	base := defaultListLabels()
	v.Labels = ListLabels{
		Filters:      text(TextKeyFilters, base.Filters),
		Rating:       text(TextKeyRating, base.Rating),
		Price:        text(TextKeyPrice, base.Price),
		PriceMin:     text(TextKeyPriceMin, base.PriceMin),
		PriceMinAria: text(TextKeyPriceMinAria, base.PriceMinAria),
		PriceMax:     text(TextKeyPriceMax, base.PriceMax),
		PriceMaxAria: text(TextKeyPriceMaxAria, base.PriceMaxAria),
		PriceApply:   text(TextKeyPriceApply, base.PriceApply),
		Sort:         text(TextKeySort, base.Sort),
		PageSize:     text(TextKeyPageSize, base.PageSize),
		View:         text(TextKeyView, base.View),
		OnSale:       text(TextKeyOnSale, base.OnSale),
		Pager:        text(TextKeyPager, base.Pager),
		Prev:         text(TextKeyPrev, base.Prev),
		Next:         text(TextKeyNext, base.Next),
		PageCurrent:  text(TextKeyPageCurrent, base.PageCurrent),
	}
	v.PageText = fmt.Sprintf(v.Labels.PageCurrent, strconv.Itoa(v.Page))
}
