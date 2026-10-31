package inventorymodel

import "testing"

func TestNormalizeStockTrackingCoercesQuantityButKeepsInfiniteZero(t *testing.T) {
	cases := []struct {
		name      string
		in        StockEntity
		wantTrack bool
		wantQty   int
	}{

		{"不跟踪 + 7 件 → 改判为跟踪行", StockEntity{TrackQuantity: false, Quantity: 7}, true, 7},

		{"不跟踪 + 0 → 仍是不跟踪（无限）", StockEntity{TrackQuantity: false, Quantity: 0}, false, 0},

		{"已跟踪 + 7 → 原样", StockEntity{TrackQuantity: true, Quantity: 7}, true, 7},
		{"已跟踪 + 0 → 原样（卖光也是跟踪态）", StockEntity{TrackQuantity: true, Quantity: 0}, true, 0},
	}
	for _, c := range cases {
		e := c.in
		normalizeStockTracking(&e)
		if e.TrackQuantity != c.wantTrack || e.Quantity != c.wantQty {
			t.Fatalf("%s：期望 track=%v quantity=%d，实际 track=%v quantity=%d",
				c.name, c.wantTrack, c.wantQty, e.TrackQuantity, e.Quantity)
		}
	}

	normalizeStockTracking(nil)
}
