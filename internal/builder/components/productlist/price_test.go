package productlist

import (
	"strings"
	"testing"
)

// TestPriceSectionRendersRangesAndSlider 价格块（issue #28）：预设档位 + 可拖动滑块。
//
// 滑块挂在原生 range 上：它是触屏与键盘天然可用的控件（拖拽 / 方向键），
// 所以这里的断言是「表单两条路都在」—— hx-get（局部刷新）与 action（无 JS 的原生 GET）。
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
	// 未设价格时两个端点回落到边界（输入框里总得显示个确切值）。
	if view.PriceFromValue != "0" || view.PriceToValue != "1000" {
		t.Fatalf("未选价格时端点应回落到边界，实际 %s / %s", view.PriceFromValue, view.PriceToValue)
	}
	for _, opt := range view.PriceOptions {
		if strings.Contains(opt.PushURL, "nodeId") || strings.Contains(opt.PushURL, "projectId") {
			t.Fatalf("档位链接的推送 URL 不该带实例配置: %s", opt.PushURL)
		}
		if !strings.Contains(opt.FragmentGet, "nodeId=list1") {
			t.Fatalf("档位链接的片段请求必须带实例配置: %s", opt.FragmentGet)
		}
	}
}

// TestPriceSectionActiveRange 当前生效的档位要高亮，再点一次 = 取消。
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

// TestPriceSectionSliderOff 关闭滑块后档位照旧（两件事互不牵连）。
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

// TestPriceConfigRejected 价格配置写错即报错（静默忽略会让作者以为配上了）。
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
