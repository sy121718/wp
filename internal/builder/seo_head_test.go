package builder

import (
	"strings"
	"testing"
)

// TestBuildSEOHeadProductJSONLD 商品页 settings.seo.productOffer 应写入 JSON-LD offers。
func TestBuildSEOHeadProductJSONLD(t *testing.T) {
	head := BuildSEOHead(SEO{
		SchemaType: "product",
		ProductOffer: &ProductOfferLD{
			SKU:           "SZ_TEE_001",
			Price:         "99",
			PriceCurrency: "CNY",
			Availability:  "InStock",
			RatingValue:   4.5,
			RatingCount:   12,
		},
	}, "/products/summer-shirt", "夏季衬衫", "纯棉透气", "首页", nil)

	for _, want := range []string{
		"<script type=\"application/ld+json\">",
		"\"@type\":\"Product\"",
		"\"sku\":\"SZ_TEE_001\"",
		"\"offers\":",
		"\"@type\":\"Offer\"",
		"\"price\":\"99\"",
		"\"priceCurrency\":\"CNY\"",
		"\"availability\":\"https://schema.org/InStock\"",
		"\"url\":\"/products/summer-shirt\"",
		"\"aggregateRating\":",
		"\"ratingValue\":4.5",
		"\"reviewCount\":12",
	} {
		if !strings.Contains(head, want) {
			t.Fatalf("SEO 头缺少 %q\n实际: %s", want, head)
		}
	}
}
