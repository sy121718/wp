package orderservice

import (
	"testing"
)

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

		{"非法值不筛", "nope", 0, "", ""},

		{"混入非法值只留合法", "paid,nope", 1, "paid", "paid"},

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

func TestSalesStatusWhitelistExcludesCancelledAndRefunded(t *testing.T) {
	for _, st := range salesStatusWhitelist {
		if st == "cancelled" || st == "refunded" {
			t.Errorf("白名单不该包含 %q（不是消费）", st)
		}
	}
}

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

	if got := ratioFloat(100, 3); got != 33.33 {
		t.Errorf("ratioFloat(100, 3) = %v，期望 33.33", got)
	}
}

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
