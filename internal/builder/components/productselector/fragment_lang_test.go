package productselector

import (
	"strings"
	"testing"
)

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
		if !strings.Contains(vo.StockFactsURL, "lang=en-US") {
			t.Errorf("可用量片段 URL 应带语言，实际 %q", vo.StockFactsURL)
		}
	}
}

func TestBuildViewFragmentURLsOmitEmptyLang(t *testing.T) {
	p := decodePropsOf(t, map[string]any{})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": testOptions, "product.variants": testVariants,
	}}, "proj-9", "")
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	for _, vo := range view.VariantOptions {
		if strings.Contains(vo.StockFactsURL, "lang=") {
			t.Errorf("空语言不该拼出 lang 参数，实际 %q", vo.StockFactsURL)
		}
	}
}
