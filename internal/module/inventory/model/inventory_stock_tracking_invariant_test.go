package inventorymodel

// inventory_stock_tracking_invariant_test.go — 钉住迁移 261 的不变量在 model 层被兜住：
// **不跟踪（无限）的行不允许带非零数量**（DDL 侧是 CHECK (track_quantity OR quantity = 0)）。
//
// 为什么必须在这一层兜：直接调 model 写入路径（不走 service）时，调用方常常只给 Quantity
// 而不翻 TrackQuantity —— 它的零值就是 false（「不填 = 无限」的默认值）。不兜的话，矛盾状态
// 会以 SQLSTATE 23514 的形态从 DDL 冒出来（页面侧看到的是内部错误），而不是被消解成一个
// 可解释的状态。真跑到过：INSERT ... track_quantity=false, quantity=7 → 23514。

import "testing"

func TestNormalizeStockTrackingCoercesQuantityButKeepsInfiniteZero(t *testing.T) {
	cases := []struct {
		name      string
		in        StockEntity
		wantTrack bool
		wantQty   int
	}{
		// 矛盾组合：数量这一列只对跟踪行有意义，写下 7 就是在说「这行有 7 件货」。
		{"不跟踪 + 7 件 → 改判为跟踪行", StockEntity{TrackQuantity: false, Quantity: 7}, true, 7},
		// 0 不是「给了数量」：不跟踪 + 0 就是「无限」，绝不能改判成「卖光了」。
		{"不跟踪 + 0 → 仍是不跟踪（无限）", StockEntity{TrackQuantity: false, Quantity: 0}, false, 0},
		// 已跟踪的行原样保留：0 是合法的显式值（卖光了），7 是正常跟踪数量。
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
	// nil 安全：这个 helper 直接挂在插入路径上，不该因为一个 nil 实体 panic。
	normalizeStockTracking(nil)
}
