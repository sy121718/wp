package productservice

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	json "go_wp/pkg/jsonx"
	"testing"
)

func TestPricingDifferenceIsBusinessResult(t *testing.T) {
	svc := &Service{}
	for _, tc := range []struct {
		old, amount, diff float64
		status            string
	}{
		{12.3, 12.4, .1, "changed"}, {12.3, 12.3, 0, "unchanged"}, {66.99, 12.5, -54.49, "changed"},
	} {
		params, err := json.Marshal(map[string]float64{"amount": tc.amount})
		if err != nil {
			t.Fatal(err)
		}
		req, err := normalizePricingRequest(&productdto.PricingRuleReq{RuleType: "fixed_price", RuleParams: params, Scope: "sku", TargetID: "v1"})
		if err != nil {
			t.Fatal(err)
		}
		lines, _, err := svc.computePricingLines(context.Background(), req, []*pricingTarget{{product: &productmodel.ProductEntity{ID: "p1"}, variant: &productmodel.VariantEntity{ID: "v1", Price: tc.old}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 1 || lines[0].Diff != tc.diff || lines[0].Status != tc.status {
			t.Fatalf("差值/状态应来自相同算价结果：%+v", lines)
		}
	}
}
