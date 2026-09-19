package productselector

// fragment_lang_test.go — 片段请求 URL 必须带语言（I18N-011）。
//
// 规格选择器与商品详情共用同一份 ParseVariantOptions，片段 URL 也是同一对：
// 语言只能从 lang 参数来，不带就恒回落工程默认语言（英文站刷新一次换成中文文案）。
// 空语言（单语言站点 / 独立编译）不带该参数。

import (
	"strings"
	"testing"
)

// TestBuildViewFragmentURLsCarryLang 两个片段位都带上构建语言。
func TestBuildViewFragmentURLsCarryLang(t *testing.T) {
	p := decodePropsOf(t, map[string]any{})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": testOptions, "product.variants": testVariants,
	}}, "proj-9", "en-US")
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
	}
}

// TestBuildViewFragmentURLsOmitEmptyLang 语言为空时不带该参数。
func TestBuildViewFragmentURLsOmitEmptyLang(t *testing.T) {
	p := decodePropsOf(t, map[string]any{})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": testOptions, "product.variants": testVariants,
	}}, "proj-9", "")
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	for _, vo := range view.VariantOptions {
		if strings.Contains(vo.StockAvailabilityGet, "lang=") {
			t.Errorf("空语言不该拼出 lang 参数，实际 %q", vo.StockAvailabilityGet)
		}
	}
}
