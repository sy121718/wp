package productlist

import (
	"strings"
	"testing"
)

func TestPriceSectionRendersRangesAndSlider(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")), map[string]any{
		"priceRanges": "0-199,200-399,799+",
	}, "")
	if !view.HasPriceSection || len(view.PriceOptions) != 3 {
		t.Fatalf("应渲染 3 个预设档位: %+v", view.PriceOptions)
	}
	if !view.ShowPriceSlider {
		t.Fatalf("有档位时应同时渲染滑块")
	}
	if view.PriceBoundMin != "0" || view.PriceBoundMax != "1000" {
		t.Fatalf("默认滑块边界应为 0~1000，实际 %s~%s", view.PriceBoundMin, view.PriceBoundMax)
	}

	if view.PriceFromValue != "0" || view.PriceToValue != "1000" {
		t.Fatalf("未选价格时端点应回落到边界，实际 %s / %s", view.PriceFromValue, view.PriceToValue)
	}
	for _, opt := range view.PriceOptions {
		if strings.Contains(opt.PushURL, "nodeId") || strings.Contains(opt.PushURL, "projectId") {
			t.Fatalf("档位链接的推送 URL 不该带实例配置: %s", opt.PushURL)
		}
		if strings.Contains(opt.ActionQuery, "nodeId=") || strings.Contains(opt.ActionQuery, "projectId=") {
			t.Fatalf("档位数据请求不应带作者展示配置: %s", opt.ActionQuery)
		}
	}
}

func TestPriceSectionActiveRange(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")), map[string]any{
		"priceRanges":    "0-199,200-399",
		"filterMinPrice": "200", "filterMaxPrice": "399",
	}, "minPrice=200&maxPrice=399")
	if len(view.PriceOptions) != 2 {
		t.Fatalf("应有 2 档: %+v", view.PriceOptions)
	}
	var active, other ControlOption
	for _, opt := range view.PriceOptions {
		if opt.Active {
			active = opt
		} else {
			other = opt
		}
	}
	if active.Label != "200-399" {
		t.Fatalf("当前档位应高亮: %+v", view.PriceOptions)
	}
	if strings.Contains(active.PushURL, "minPrice=200") {
		t.Fatalf("再点已选档位应清除价格条件: %s", active.PushURL)
	}
	if !strings.Contains(other.PushURL, "minPrice=0") {
		t.Fatalf("点另一档应切到该档: %s", other.PushURL)
	}
	if view.PriceFromValue != "200" || view.PriceToValue != "399" {
		t.Fatalf("滑块应回填当前区间，实际 %s / %s", view.PriceFromValue, view.PriceToValue)
	}
}

func TestPriceSectionSliderOff(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")), map[string]any{
		"priceRanges": "0-199", "priceSlider": "off",
	}, "")
	if !view.HasPriceSection || len(view.PriceOptions) != 1 {
		t.Fatalf("档位应照旧渲染: %+v", view)
	}
	if view.ShowPriceSlider {
		t.Fatalf("priceSlider=off 时不该渲染滑块")
	}
}

func TestPriceConfigRejected(t *testing.T) {
	cases := []struct{ name, key, value string }{
		{"档位缺上限", "priceRanges", "200-399,bad"},
		{"档位负值", "priceRanges", "-10-50"},
		{"档位上下限颠倒", "priceRanges", "399-200"},
		{"滑块边界形状非法", "priceBounds", "100"},
		{"滑块边界上下限颠倒", "priceBounds", "500,100"},
		{"滑块开关非法", "priceSlider", "yes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := decodePropsOf(t, map[string]any{c.key: c.value, "titleField": "item.name"})
			if err := validateExtra(&p, "n1"); err == nil {
				t.Fatalf("应被拒绝: %s=%s", c.key, c.value)
			}
		})
	}
	good := decodePropsOf(t, map[string]any{"priceRanges": "0-199,799+", "priceBounds": "0,2000", "titleField": "item.name"})
	if err := validateExtra(&good, "n1"); err != nil {
		t.Fatalf("合法价格配置不该报错: %v", err)
	}
}

func TestRatingSection(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")), map[string]any{
		"ratingOptions": "4.5,4,3", "filterMinRating": "4",
	}, "minRating=4")
	if !view.HasRatingSection || len(view.RatingOptions) != 3 {
		t.Fatalf("应渲染 3 个评分档位: %+v", view.RatingOptions)
	}
	var active ControlOption
	for _, opt := range view.RatingOptions {
		if opt.Active {
			active = opt
		}
	}
	if active.Label != "≥ 4 星" {
		t.Fatalf("当前评分档应高亮: %+v", view.RatingOptions)
	}
	if strings.Contains(active.PushURL, "minRating=4") {
		t.Fatalf("再点已选档位应清除评分条件: %s", active.PushURL)
	}

	plain := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")), map[string]any{}, "")
	if plain.HasRatingSection {
		t.Fatalf("没配 ratingOptions 时不该渲染评分块")
	}
}

func TestRatingSortPutsUnratedLast(t *testing.T) {
	rated := itemOf("rated", "已评分", "2026-01-01T00:00:00Z")
	rated["ratingValue"] = 4.25
	unrated := itemOf("unrated", "未评分", "2026-01-02T00:00:00Z")
	zero := itemOf("zero", "零分", "2026-01-03T00:00:00Z")
	zero["ratingValue"] = 0.0

	view := buildViewOf(t, itemsColl(unrated, zero, rated), map[string]any{
		"toolbar": "sort", "orderBy": "ratingDesc", "titleField": "item.name",
	}, "")
	order := []string{}
	for _, card := range view.Cards {
		order = append(order, card.Title)
	}
	if len(order) != 3 || order[0] != "已评分" || order[len(order)-1] != "未评分" {
		t.Fatalf("评分降序应把无评分的排最后，实际 %v", order)
	}
}

func TestPriceSortOrdersByMinPrice(t *testing.T) {
	cheap := itemOf("cheap", "便宜", "2026-01-01T00:00:00Z")
	cheap["minPrice"] = 50.0
	pricey := itemOf("pricey", "贵", "2026-01-02T00:00:00Z")
	pricey["minPrice"] = 300.0
	none := itemOf("none", "无价", "2026-01-03T00:00:00Z")

	asc := buildViewOf(t, itemsColl(none, pricey, cheap), map[string]any{
		"toolbar": "sort", "orderBy": "priceAsc", "titleField": "item.name",
	}, "")
	if len(asc.Cards) != 3 || asc.Cards[0].Title != "便宜" || asc.Cards[2].Title != "无价" {
		t.Fatalf("价格升序不正确（无价应排最后）: %+v", asc.Cards)
	}

	desc := buildViewOf(t, itemsColl(none, cheap, pricey), map[string]any{
		"toolbar": "sort", "orderBy": "priceDesc", "titleField": "item.name",
	}, "")
	if len(desc.Cards) != 3 || desc.Cards[0].Title != "贵" || desc.Cards[2].Title != "无价" {
		t.Fatalf("价格降序不正确（无价应排最后）: %+v", desc.Cards)
	}
}
