package product

import (
	"strings"
	"testing"
)

const (
	langTestOptions  = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
	langTestVariants = `[{"id":"var-1","sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}},` +
		`{"id":"var-2","sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`
)

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
		if !strings.Contains(vo.StockFactsURL, "lang=en-US") {
			t.Errorf("可用量片段 URL 应带语言，实际 %q", vo.StockFactsURL)
		}
		if vo.PriceFactsURL != "" && !strings.Contains(vo.PriceFactsURL, "lang=en-US") {
			t.Errorf("价格核对片段 URL 应带语言，实际 %q", vo.PriceFactsURL)
		}
	}
}

func TestBuildViewFragmentURLsOmitEmptyLang(t *testing.T) {
	p := optionProps(t)
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": langTestOptions, "product.variants": langTestVariants,
	}}, "proj-1", "", nil)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	for _, vo := range view.VariantOptions {
		if strings.Contains(vo.StockFactsURL, "lang=") {
			t.Errorf("空语言不该拼出 lang 参数，实际 %q", vo.StockFactsURL)
		}
		if strings.Contains(vo.PriceFactsURL, "lang=") {
			t.Errorf("空语言不该拼出 lang 参数，实际 %q", vo.PriceFactsURL)
		}
	}
}
