package orderservice

import ordermodel "go_wp/internal/module/order/model"

// allocateLineDiscounts 把订单级折扣按行小计比例分摊到各订单项。
//
// 最后一行吸收舍入差，保证各行 LineDiscount 之和等于 discount。
// 退款按 LineTotal 算，避免「券减在头上、退按原价」的资损。
func allocateLineDiscounts(items []*ordermodel.OrderItemEntity, subtotal, discount int64) {
	if len(items) == 0 {
		return
	}
	if discount <= 0 || subtotal <= 0 {
		for _, it := range items {
			it.LineDiscount = 0
			it.LineTotal = it.LineSubtotal
		}
		return
	}
	var allocated int64
	last := len(items) - 1
	for i, it := range items {
		if i == last {
			it.LineDiscount = discount - allocated
		} else {
			it.LineDiscount = discount * it.LineSubtotal / subtotal
			allocated += it.LineDiscount
		}
		it.LineTotal = it.LineSubtotal - it.LineDiscount
	}
}

// lineRefundAmount 按分摊后的行实付额计算部分退货应退金额（分）。
func lineRefundAmount(it *ordermodel.OrderItemEntity, qty int) int64 {
	if it == nil || qty <= 0 {
		return 0
	}
	if it.Quantity <= 0 {
		return 0
	}
	return it.LineTotal * int64(qty) / int64(it.Quantity)
}
