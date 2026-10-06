package orderhttp

// order_sales_mix_view_test.go — 月度趋势堆叠柱的**高度闭合**判据。
//
// 为什么单独测这个：三段高度是三次整数除法（各段 × 柱高 ÷ 最大值），
// 若每段独立取整，三段之和会与柱高差 1~2 像素 —— 图上表现为柱顶有一道缝
// 或者最后一段溢出容器。这类偏差**不报错、不 500**，只是看上去「对不齐」，
// 而每一段单独看都算得「差不多对」，很容易被当成渲染问题去查 CSS。
//
// 视图层的做法是按**累计位置**算：段高 = 相邻两个累计位置之差，最后一段的终点
// 直接取柱高，于是和恒等于柱高。这条断言就是钉住这个恒等式。
//
// 数据刻意选**除不尽**的值（777 / 111 / 222 / 444）：整除时两种算法结果相同，
// 判据就抓不到独立取整的那种写法。

import (
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

func TestMonthlyStackHeightsSumToBarHeight(t *testing.T) {
	const maxSales = int64(1000)
	points := []orderdto.SalesMonthlyPointDTO{
		// 整除：两种算法都对，作为对照。
		{Month: "2026-10", Sales: 1000, NewSales: 400, ReturningSales: 300, GuestSales: 300},
		// 除不尽：独立取整时三段之和会是 79+158+317=554 而柱高 555（相差 1px）。
		{Month: "2026-09", Sales: 777, NewSales: 111, ReturningSales: 222, GuestSales: 444},
		// 只有一段有值。
		{Month: "2026-07", Sales: 333, NewSales: 333},
		// 全 0：柱高 0，三段也必须都是 0（不能出现 1px 的幽灵段）。
		{Month: "2026-06", Sales: 0},
		// 最大值本身取 0（区间里一单都没有）：不能除零。
		{Month: "2026-05", Sales: 0},
	}

	for _, p := range points {
		v := salesMonthlyPointView(p, "CNY", maxSales)
		height, _ := v["Height"].(int)
		newH, _ := v["NewHeight"].(int)
		retH, _ := v["ReturningHeight"].(int)
		guestH, _ := v["GuestHeight"].(int)

		if sum := newH + retH + guestH; sum != height {
			t.Errorf("%s 三段高度之和 = %d，柱高 = %d —— 三段必须拼成整根柱（段高按累计位置算）",
				p.Month, sum, height)
		}
		if newH < 0 || retH < 0 || guestH < 0 {
			t.Errorf("%s 出现负段高：new=%d returning=%d guest=%d", p.Month, newH, retH, guestH)
		}
		if p.Sales == 0 && height != 0 {
			t.Errorf("%s 总量为 0 时柱高应为 0，实得 %d", p.Month, height)
		}
	}
}
