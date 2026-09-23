package product

// fragment_lang_test.go — 片段请求 URL 必须带语言（I18N-011）。
//
// 片段语言**只从 lang 参数来**：端点不读 Accept-Language、不读任何语言 cookie
// （runtimefragment.resolveRequestLang）。不带 lang 的请求恒回落**工程默认语言** ——
// 英文站的商品页刷新一次，实时价格 / 可用量位就换成中文词条，页面上看不出错。
//
// 语言为空（单语言站点 / 独立编译）时**不带**该参数：空值只让片段多做一次无用判断。

import (
	"strings"
	"testing"
)

const (
	langTestOptions  = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
	langTestVariants = `[{"id":"var-1","sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}},` +
		`{"id":"var-2","sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`
)

// TestBuildViewFragmentURLsCarryLang 两个片段位都带上构建语言。
func TestBuildViewFragmentURLsCarryLang(t *testing.T) {
	p := optionProps(t)
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": langTestOptions, "product.variants": langTestVariants,
	}}, "proj-1", "en-US", nil)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.VariantOptions) == 0 {
		t.Fatalf("应有组合行")
	}
	for _, vo := range view.VariantOptions {
		if !strings.Contains(vo.StockAvailabilityGet, "lang=en-US") {
			t.Errorf("可用量片段 URL 应带语言，实际 %q", vo.StockAvailabilityGet)
		}
		if vo.LivePriceGet != "" && !strings.Contains(vo.LivePriceGet, "lang=en-US") {
			t.Errorf("价格核对片段 URL 应带语言，实际 %q", vo.LivePriceGet)
		}
	}
}

// TestBuildViewFragmentURLsOmitEmptyLang 语言为空时不带该参数（单语言站点形态）。
func TestBuildViewFragmentURLsOmitEmptyLang(t *testing.T) {
	p := optionProps(t)
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": langTestOptions, "product.variants": langTestVariants,
	}}, "proj-1", "", nil)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	for _, vo := range view.VariantOptions {
		if strings.Contains(vo.StockAvailabilityGet, "lang=") {
			t.Errorf("空语言不该拼出 lang 参数，实际 %q", vo.StockAvailabilityGet)
		}
		if strings.Contains(vo.LivePriceGet, "lang=") {
			t.Errorf("空语言不该拼出 lang 参数，实际 %q", vo.LivePriceGet)
		}
	}
}
