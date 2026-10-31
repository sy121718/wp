package productservice

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func TestAttributeQueryVariationNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"1", "1"}, {"0", "0"}, {"yes", ""}, {"  ", ""}} {
		q := normalizedTaxonomyQuery(&productdto.CatalogTaxonomyReq{Variation: tc.in, Page: 999})
		if q.Variation != tc.want {
			t.Fatalf("归一结果=%q，want %q", q.Variation, tc.want)
		}
		result, err := (&Service{}).QueryAttributes(context.Background(), &q)
		if err != nil || len(result.Items) != 0 || result.Page != 1 {
			t.Fatalf("空工程不得查库: %+v %v", result, err)
		}
	}
}
