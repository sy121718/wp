package builder_test

// seo_currency_test.go — JSON-LD 的 priceCurrency 跟随全局默认币种（Y2 的展示层接入）。
//
// 判据是**渲染出的片段**：结构化数据里写死 CNY、而页面 props 用符号（¥ / $）是两处
// 自相矛盾的来源 —— 接上同一个来源后这个裂口不会再出现。
// 币种来自 pkg/i18n 的进程内缓存值，这里用 SetValueLoader 注入来驱动。

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder"
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
		// schemaType 必须是 product：offer 只在商品页输出（非商品页带 Offer
		// 是结构化数据造假，搜索引擎会判 spam）。
		SchemaType: "product",
		ProductOffer: &builder.ProductOfferLD{
			SKU:   "SKU-1",
			Price: "19.90",
			// 刻意留空：走**兜底**分支（offer.PriceCurrency 为空时取全局默认）
			Availability: "InStock",
		},
	}
	head := builder.BuildSEOHead(seo, "https://example.com/p/1", "", "", "", nil)
	if !strings.Contains(head, `"priceCurrency":"USD"`) {
		t.Fatalf("JSON-LD 的 priceCurrency 应取全局默认币种（USD）：\n%s", head)
	}
	t.Logf("JSON-LD 片段：%s", jsonLDSnippet(head))
}

// jsonLDSnippet 截出 JSON-LD 里 offers 那一段（贴报告用）。
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
