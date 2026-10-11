package templates

import "testing"

// Exercise the production partial against each unchanged order-page base.
func TestOrderPaginationPartialFactsMatchLegacyBytes(t *testing.T) {
	sites := []struct{ name, base string }{
		{"orders filtered", "/admin/orders?keyword=A%2B%26&paymentMethod=paypal&project=p-1&status=pending"},
		{"orders plain", "/admin/orders?project=p-1"},
		{"coupons filtered", "/admin/coupons?keyword=A%2B%26&project=p-1&status=enabled"},
		{"coupons plain", "/admin/coupons?project=p-1"},
		{"returns filtered", "/admin/returns?keyword=A%2B%26&project=p-1&status=requested"},
		{"returns plain", "/admin/returns?project=p-1"},
	}
	shapes := append(b3Shapes(), struct {
		name        string
		total       int64
		page, limit int
	}{"single", 5, 1, 20}, struct {
		name        string
		total       int64
		page, limit int
	}{"empty", 0, 1, 20})
	for _, site := range sites {
		for _, shape := range shapes {
			t.Run(site.name+"/"+shape.name, func(t *testing.T) {
				s := p0PaginationShape{name: shape.name, total: shape.total, page: shape.page, limit: shape.limit, base: site.base}
				legacy, facts := p2RenderLegacy(t, s), p2RenderFacts(t, s, nil)
				if legacy != facts {
					t.Fatalf("partial bytes changed: %s", p2FirstDiff(legacy, facts))
				}
			})
		}
	}
}
