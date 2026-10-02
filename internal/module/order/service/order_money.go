package orderservice

import ordermodel "go_wp/internal/module/order/model"

// allocateLineDiscounts 把订单级折扣按行小计比例分摊到各订单项。
//
// 分摊的目标有两条，**同时**成立才算分对：
//
//  1. 各行 LineDiscount 之和等于实际吃掉的折扣（退款按 LineTotal 算，
//     避免「券减在头上、退按原价」的资损）；
//  2. 任何一行都不得被折扣吃穿 —— LineTotal >= 0（BIZ-08）。
//
// 第 2 条不是理论洁癖：末行无条件吸收舍入差会溢出小计。复算用例：3 行小计
// 34/33/33（subtotal=100 分）+ fixed=99 分券 → 前两行 floor 分配 33/32（allocated=65）
// → 末行 99-65=34 > 33 ⇒ 旧实现给出 LineTotal = -1，部分退货于是显示「应退 -0.01 元」。
//
// 做法：末行优先吸收舍入差；**溢出时不硬塞**，而是把溢出额回摊到前面的行
// （从最后一行往前，逐行填到它的剩余额度为止）。回摊顺序从后往前与「末行吸收」
// 同一方向，逐行填满即停，结果与入参顺序、与浮点无关（全整数运算，确定性）。
//
// 折扣总额超过小计之和时（建单侧已夹取，这里独立守住口径）仍会剩下填不完的额度，
// 直接丢弃：宁可少减折扣，也不能产生负的行实付 —— 后者会在退款链路上变成凭空补偿。
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
	last := len(items) - 1
	var allocated int64
	for i, it := range items {
		if i == last {
			continue // 末行最后单独算（先看它能不能吃下剩余的舍入差）
		}
		it.LineDiscount = discount * it.LineSubtotal / subtotal
		if it.LineDiscount > it.LineSubtotal {
			// 防御：比例分配在 subtotal > 各行小计之和时可能溢出（口径异常，不静默吃穿）。
			it.LineDiscount = it.LineSubtotal
		}
		allocated += it.LineDiscount
	}

	rest := discount - allocated
	if rest < 0 {
		rest = 0
	}
	items[last].LineDiscount = min64(rest, items[last].LineSubtotal)
	// 末行吃不下的部分回摊到前面的行（从后往前，逐行填到它的剩余额度为止）。
	deficit := rest - items[last].LineDiscount
	for i := last - 1; i >= 0 && deficit > 0; i-- {
		room := items[i].LineSubtotal - items[i].LineDiscount
		if room <= 0 {
			continue
		}
		add := min64(room, deficit)
		items[i].LineDiscount += add
		deficit -= add
	}

	for _, it := range items {
		// 收口：任何一行都不得为负（回摊与比例分配都已夹取，这里是最后一道）。
		if it.LineDiscount < 0 {
			it.LineDiscount = 0
		}
		if it.LineDiscount > it.LineSubtotal {
			it.LineDiscount = it.LineSubtotal
		}
		it.LineTotal = it.LineSubtotal - it.LineDiscount
	}
}

// min64 取较小值（分摊里的夹取用，避免为两行引入 math.Min）。
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
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
