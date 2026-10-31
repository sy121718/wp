package orderservice

import (
	"testing"

	ordermodel "go_wp/internal/module/order/model"
)

func itemsOf(subtotals ...int64) []*ordermodel.OrderItemEntity {
	out := make([]*ordermodel.OrderItemEntity, 0, len(subtotals))
	for _, s := range subtotals {
		out = append(out, &ordermodel.OrderItemEntity{LineSubtotal: s, Quantity: 1})
	}
	return out
}

func assertLineInvariants(t *testing.T, items []*ordermodel.OrderItemEntity, subtotal, discount int64) {
	t.Helper()
	var discSum, totalSum, subtotalSum int64
	for i, it := range items {
		if it.LineDiscount < 0 {
			t.Errorf("第 %d 行折扣为负: %d", i+1, it.LineDiscount)
		}
		if it.LineTotal < 0 {
			t.Errorf("第 %d 行实付为负: %d（小计 %d、折扣 %d）—— 退款会算出负的应退金额",
				i+1, it.LineTotal, it.LineSubtotal, it.LineDiscount)
		}
		if it.LineDiscount > it.LineSubtotal {
			t.Errorf("第 %d 行折扣超过小计: 折扣 %d > 小计 %d", i+1, it.LineDiscount, it.LineSubtotal)
		}
		discSum += it.LineDiscount
		totalSum += it.LineTotal
		subtotalSum += it.LineSubtotal
	}
	if totalSum != subtotalSum-discSum {
		t.Errorf("行实付之和 %d != 小计之和 %d - 折扣之和 %d", totalSum, subtotalSum, discSum)
	}

	if discount > 0 && discount <= subtotal && discSum != discount {
		t.Errorf("折扣之和 %d != 订单折扣 %d（券的账不平）", discSum, discount)
	}
}

func TestAllocateLineDiscountsOverflowCase(t *testing.T) {
	items := itemsOf(34, 33, 33)
	allocateLineDiscounts(items, 100, 99)
	assertLineInvariants(t, items, 100, 99)

	for i, it := range items {
		if got := lineRefundAmount(it, it.Quantity); got < 0 {
			t.Errorf("第 %d 行退货应退金额为负: %d", i+1, got)
		}
	}
}

func TestAllocateLineDiscountsAnyCombination(t *testing.T) {
	cases := 0
	for a := int64(1); a <= 12; a += 3 {
		for b := int64(1); b <= 12; b += 3 {
			for c := int64(1); c <= 12; c += 3 {
				for _, d := range []int64{1, 3, 5, 9, 13, 17, 23, 29, 36} {
					items := itemsOf(a, b, c)
					allocateLineDiscounts(items, a+b+c, d)
					assertLineInvariants(t, items, a+b+c, d)
					cases++
				}
			}
		}
	}
	if cases < 100 {
		t.Fatalf("只跑了 %d 个组合，穷举范围写窄了", cases)
	}
}

func TestAllocateLineDiscountsDiscountBeyondSubtotal(t *testing.T) {
	items := itemsOf(30, 20, 10)
	allocateLineDiscounts(items, 60, 100)
	for i, it := range items {
		if it.LineTotal < 0 {
			t.Errorf("第 %d 行实付为负: %d", i+1, it.LineTotal)
		}
		if it.LineDiscount > it.LineSubtotal {
			t.Errorf("第 %d 行折扣超过小计: %d > %d", i+1, it.LineDiscount, it.LineSubtotal)
		}
	}
}

func TestAllocateLineDiscountsKeepsExactSplit(t *testing.T) {
	items := itemsOf(6000, 4000)
	allocateLineDiscounts(items, 10000, 2000)
	if items[0].LineDiscount != 1200 || items[1].LineDiscount != 800 {
		t.Fatalf("整除场景被改写: %d / %d", items[0].LineDiscount, items[1].LineDiscount)
	}
	assertLineInvariants(t, items, 10000, 2000)
}

func TestAllocateLineDiscountsZeroAndNegative(t *testing.T) {
	items := itemsOf(10, 20)
	allocateLineDiscounts(items, 30, 0)
	assertLineInvariants(t, items, 30, 0)

	allocateLineDiscounts(nil, 0, 0)
	zero := itemsOf(0, 0, 0)
	allocateLineDiscounts(zero, 0, 5)
	for i, it := range zero {
		if it.LineTotal != 0 || it.LineDiscount != 0 {
			t.Errorf("零小计行不该分到折扣: 第 %d 行 %+v", i+1, it)
		}
	}
}
