package productlist

import (
	"fmt"
	"strconv"
)

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

	fallbackPageCurrent = "第 %s 页"
)

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
