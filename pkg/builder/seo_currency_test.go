package builder_test

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/builder"
	"go_wp/pkg/i18n"
)

func TestSEOPriceCurrencyFollowsGlobalDefault(t *testing.T) {
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{DefaultCurrency: "USD"}, nil
	})
	defer i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{}, nil
	})
	if got := i18n.GetDefaultCurrency(); got != "USD" {
		t.Fatalf("进程内缓存值应为 USD，实际 %s", got)
	}

	seo := builder.SEO{
		Title: "商品页",

		SchemaType: "product",
		ProductOffer: &builder.ProductOfferLD{
			SKU:   "SKU-1",
			Price: "19.90",

			Availability: "InStock",
		},
	}
	head := builder.BuildSEOHead(seo, "https://example.com/p/1", "", "", "", nil)
	if !strings.Contains(head, `"priceCurrency":"USD"`) {
		t.Fatalf("JSON-LD 的 priceCurrency 应取全局默认币种（USD）：\n%s", head)
	}
	t.Logf("JSON-LD 片段：%s", jsonLDSnippet(head))
}

func jsonLDSnippet(head string) string {
	idx := strings.Index(head, `"priceCurrency"`)
	if idx < 0 {
		return "(未找到 priceCurrency)"
	}
	start := idx - 60
	if start < 0 {
		start = 0
	}
	end := idx + 40
	if end > len(head) {
		end = len(head)
	}
	return strings.TrimSpace(head[start:end])
}
