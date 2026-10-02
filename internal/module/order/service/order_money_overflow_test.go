package orderservice

// order_money_overflow_test.go — 折扣分摊的**不变量**测试（BIZ-08）。
//
// 缺陷：末行无条件吸收全部舍入差（`LineDiscount = discount - allocated`），
// 当它超过该行小计时把行实付打成负数，退款按 LineTotal 算于是出现「应退 -0.01 元」。
//
// 复算用例（指令书原文）：3 行小计 34/33/33（subtotal=100 分）+ fixed=99 分券
// → 前两行 floor 分配 33/32（allocated=65）→ 末行 99-65=34 > 33 ⇒ LineTotal = -1。
//
// 判据是**不变量**而不是某一次的具体数值：任何入参组合下
//   ① 每行 LineTotal >= 0；
//   ② 行实付之和 = 小计之和 - 实际吃掉的折扣；
//   ③ 可行时（discount <= subtotal）折扣之和恰好等于 discount —— 券的账要平。

import (
	"testing"

	ordermodel "go_wp/internal/module/order/model"
)

// itemsOf 按小计构造订单项（数量固定 1，金额单位：分）。
func itemsOf(subtotals ...int64) []*ordermodel.OrderItemEntity {
	out := make([]*ordermodel.OrderItemEntity, 0, len(subtotals))
	for _, s := range subtotals {
		out = append(out, &ordermodel.OrderItemEntity{LineSubtotal: s, Quantity: 1})
	}
	return out
}

// assertLineInvariants 三条不变量统一断言（subtotal 为各行小计之和）。
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
	// 折扣吃满的可行区间：discount 落在 [0, subtotal] 内时必须一分不差地分完。
	if discount > 0 && discount <= subtotal && discSum != discount {
		t.Errorf("折扣之和 %d != 订单折扣 %d（券的账不平）", discSum, discount)
	}
}

// TestAllocateLineDiscountsOverflowCase 指令书的复算用例：末行折扣溢出小计。
func TestAllocateLineDiscountsOverflowCase(t *testing.T) {
	items := itemsOf(34, 33, 33)
	allocateLineDiscounts(items, 100, 99)
	assertLineInvariants(t, items, 100, 99)

	// 退款按行实付算：该行（部分）退货不得出现负的应退金额。
	for i, it := range items {
		if got := lineRefundAmount(it, it.Quantity); got < 0 {
			t.Errorf("第 %d 行退货应退金额为负: %d", i+1, got)
		}
	}
}

// TestAllocateLineDiscountsAnyCombination 小规模穷举：任意行数 / 小计 / 折扣都不得越界。
//
// 手写用例只能覆盖想到的组合；这条按步长穷举到 4 行，把「末行吸收多少都可能溢出」
// 这类形态整片盖住（分红与折扣都是分，步长 1 分足够）。
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

// TestAllocateLineDiscountsDiscountBeyondSubtotal 防御：折扣大于小计（建单侧已夹取，
// 这里独立守住口径）——总折扣不能全吃下时，各行实付仍不得为负。
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

// TestAllocateLineDiscountsKeepsExactSplit 既有口径不回退：能整除时逐行金额不变。
func TestAllocateLineDiscountsKeepsExactSplit(t *testing.T) {
	items := itemsOf(6000, 4000)
	allocateLineDiscounts(items, 10000, 2000)
	if items[0].LineDiscount != 1200 || items[1].LineDiscount != 800 {
		t.Fatalf("整除场景被改写: %d / %d", items[0].LineDiscount, items[1].LineDiscount)
	}
	assertLineInvariants(t, items, 10000, 2000)
}

// TestAllocateLineDiscountsZeroAndNegative 边界：无折扣 / 零小计 / 空列表。
func TestAllocateLineDiscountsZeroAndNegative(t *testing.T) {
	items := itemsOf(10, 20)
	allocateLineDiscounts(items, 30, 0)
	assertLineInvariants(t, items, 30, 0)

	allocateLineDiscounts(nil, 0, 0) // 不得 panic
	zero := itemsOf(0, 0, 0)
	allocateLineDiscounts(zero, 0, 5)
	for i, it := range zero {
		if it.LineTotal != 0 || it.LineDiscount != 0 {
			t.Errorf("零小计行不该分到折扣: 第 %d 行 %+v", i+1, it)
		}
	}
}
