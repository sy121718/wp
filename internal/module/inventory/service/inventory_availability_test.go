package inventoryservice

import (
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
)

func TestFiniteStockAvailabilityKeepsUnknownDistinctFromZero(t *testing.T) {
	facts := map[string]*inventorycontract.StockFact{
		"tracked": {TrackQuantity: true, Quantity: 8, ReservedQuantity: 3},
		"empty":   {TrackQuantity: true},
		"over":    {TrackQuantity: true, Quantity: 2, ReservedQuantity: 3},
		"free":    {TrackQuantity: false},
	}
	for _, tc := range []struct {
		name  string
		ids   []string
		want  int
		known bool
	}{
		{"无引用", nil, 0, false},
		{"不跟踪", []string{"free"}, 0, false},
		{"混合有限与无限", []string{"tracked", "free"}, 0, false},
		{"缺失事实", []string{"tracked", "missing"}, 0, false},
		{"跟踪售罄", []string{"empty"}, 0, true},
		{"全部预占", []string{"over"}, 0, true},
		{"有限可售合计", []string{"tracked", "empty"}, 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, known := finiteStockAvailability(tc.ids, facts)
			if got != tc.want || known != tc.known {
				t.Fatalf("可用量=(%d,%t)，期望=(%d,%t)", got, known, tc.want, tc.known)
			}
		})
	}
}
