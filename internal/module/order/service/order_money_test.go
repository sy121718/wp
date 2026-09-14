package orderservice

import (
	"testing"

	ordermodel "go_wp/internal/module/order/model"
)

func TestAllocateLineDiscounts(t *testing.T) {
	items := []*ordermodel.OrderItemEntity{
		{LineSubtotal: 6000, Quantity: 1},
		{LineSubtotal: 4000, Quantity: 1},
	}
	allocateLineDiscounts(items, 10000, 2000)
	var discSum, totalSum int64
	for _, it := range items {
		discSum += it.LineDiscount
		totalSum += it.LineTotal
	}
	if discSum != 2000 {
		t.Fatalf("折扣分摊之和应为 2000，实际 %d", discSum)
	}
	if totalSum != 8000 {
		t.Fatalf("行实付之和应为 8000，实际 %d", totalSum)
	}
}

func TestLineRefundAmountUsesLineTotal(t *testing.T) {
	it := &ordermodel.OrderItemEntity{LineSubtotal: 10000, LineTotal: 8000, Quantity: 2}
	if got := lineRefundAmount(it, 1); got != 4000 {
		t.Fatalf("退 1 件应退 4000 分，实际 %d", got)
	}
}
