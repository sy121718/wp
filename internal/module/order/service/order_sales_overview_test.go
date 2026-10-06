package orderservice

// order_sales_overview_test.go — 销售概览 service 的纯函数判据（无库）。
//
// 这一批函数的共同点是「输入怎么错都不该把页面打空」：
// 状态筛选写错 → 回落成「不筛」（而不是报错）、月数越界 → 回落默认、
// 分母为 0 → 给 0（而不是 NaN/Inf）。它们在别处（handler 的 query 解析）之后，
// 所以走到这里时输入已经是「用户随手打的字符串」。

import (
	"testing"
)

// 状态筛选：空 → **不筛**（空切片，由 model 层的 statusesText 落成默认白名单）；
// 白名单内的原样；白名单外的被**丢掉**而不是报错。
//
// 期望「非法值 → 空切片」而不是「非法值 → 全部白名单」：service 只回答「用户要筛哪些」，
// 「空 = 用默认白名单」是 model 层 statusesText 的职责 —— 把默认值在两层各写一遍，
// 改了一处另一处就静默保持旧口径。
func TestSalesStatusesFallsBackInsteadOfFailing(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantLen   int
		wantEff   string
		wantFirst string
	}{
		{"空值不筛", "", 0, "", ""},
		{"单个白名单值", "paid", 1, "paid", "paid"},
		{"大写归一", "PAID", 1, "paid", "paid"},
		{"首尾空格归一", "  shipped  ", 1, "shipped", "shipped"},
		{"中文逗号分隔", "paid，completed", 2, "paid,completed", "paid"},
		{"英文逗号分隔", "paid,completed", 2, "paid,completed", "paid"},
		{"空格分隔", "paid completed", 2, "paid,completed", "paid"},
		// 非法值**不报错**（报错会把整页换成归口文案，而用户只是把 URL 里的
		// status 打错了一个字）：全非法 → 与「没传」同义（空切片 → 不筛）。
		{"非法值不筛", "nope", 0, "", ""},
		// 混入非法值：合法的留下、非法的丢掉 —— 比「整串作废」更接近用户意图。
		{"混入非法值只留合法", "paid,nope", 1, "paid", "paid"},
		// 取消/退款**不在白名单**：它们不是「消费」，混进来会把销售额算成负向。
		{"取消不筛", "cancelled", 0, "", ""},
		{"退款不筛", "refunded", 0, "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, eff, err := salesStatuses(c.raw)
			if err != nil {
				t.Fatalf("不该报错，得到 %v", err)
			}
			if len(got) != c.wantLen {
				t.Fatalf("状态数 = %d，期望 %d（%v）", len(got), c.wantLen, got)
			}
			if eff != c.wantEff {
				t.Errorf("生效值 = %q，期望 %q", eff, c.wantEff)
			}
			if len(got) > 0 && got[0] != c.wantFirst {
				t.Errorf("首个状态 = %q，期望 %q", got[0], c.wantFirst)
			}
			if c.wantFirst == "" {
				return
			}
		})
	}
}

// 白名单里不能出现 cancelled / refunded —— 这条断言挡的是「有人往白名单里加值」。
func TestSalesStatusWhitelistExcludesCancelledAndRefunded(t *testing.T) {
	for _, st := range salesStatusWhitelist {
		if st == "cancelled" || st == "refunded" {
			t.Errorf("白名单不该包含 %q（不是消费）", st)
		}
	}
}

// 趋势月数：0/负数/超限回落默认，合法值原样。
func TestSalesMonthlyMonthsClamps(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, salesDefaultMonthly},
		{-3, salesDefaultMonthly},
		{1, 1},
		{6, 6},
		{salesMaxMonthly, salesMaxMonthly},
		{salesMaxMonthly + 1, salesMaxMonthly},
		{9999, salesMaxMonthly},
	}
	for _, c := range cases {
		if got := salesMonthlyMonths(c.in); got != c.want {
			t.Errorf("salesMonthlyMonths(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

// 比率：分母为 0 时给 0（不是 NaN / Inf —— 它们进了模板会渲染成 "NaN"）。
func TestSalesRatiosGuardZeroDenominator(t *testing.T) {
	if got := ratioCents(100, 0); got != 0 {
		t.Errorf("ratioCents(100, 0) = %d，期望 0", got)
	}
	if got := ratioFloat(100, 0); got != 0 {
		t.Errorf("ratioFloat(100, 0) = %v，期望 0", got)
	}
	if got := ratioCents(1000, 4); got != 250 {
		t.Errorf("ratioCents(1000, 4) = %d，期望 250", got)
	}
	// 四舍五入到两位：100/3 = 33.333… → 33.33
	if got := ratioFloat(100, 3); got != 33.33 {
		t.Errorf("ratioFloat(100, 3) = %v，期望 33.33", got)
	}
}

// 变化率：上一期为 0 时给 nil（**不是 0%**）。
func TestSalesChangePctNilWhenNoBase(t *testing.T) {
	if got := changePct(0, 100); got != nil {
		t.Errorf("上一期为 0 时应给 nil，得到 %v", *got)
	}
	got := changePct(100, 150)
	if got == nil {
		t.Fatal("上一期非 0 时不该给 nil")
	}
	if *got != 50 {
		t.Errorf("100 → 150 的变化率 = %v，期望 50", *got)
	}
	down := changePct(200, 100)
	if down == nil || *down != -50 {
		t.Errorf("200 → 100 的变化率 = %v，期望 -50", down)
	}
}
