package orderservice

// order_daily_series_test.go — fillDailyPoints 的纯函数测试（补零与日期推进）。
//
// 为什么单独测：这条逻辑是「柱图上有几根柱子」的唯一来源，而它错起来是**静默**的 ——
// 少一天/多一天不会报错，只会让图和 KPI 对不上。跨月与单日是最容易写错的两处
// （AddDate 在月末、以及 from/to 相等时的空循环）。

import (
	"testing"
	"time"

	ordermodel "go_wp/internal/module/order/model"
)

func TestFillDailyPoints(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	row := func(y int, m time.Month, d int, orders, paid, net int64) ordermodel.OrderDailyPoint {
		return ordermodel.OrderDailyPoint{
			Day: day(y, m, d), OrderCount: orders, PaidOrderCount: paid, NetSales: net,
		}
	}

	cases := []struct {
		name     string
		from, to time.Time
		rows     []ordermodel.OrderDailyPoint
		wantDays []string
		wantNet  []int64
	}{
		{
			name:     "区间内没有任何订单：逐日补零",
			from:     day(2026, 9, 1),
			to:       day(2026, 9, 4),
			wantDays: []string{"2026-09-01", "2026-09-02", "2026-09-03"},
			wantNet:  []int64{0, 0, 0},
		},
		{
			name:     "单日区间：一个点",
			from:     day(2026, 9, 2),
			to:       day(2026, 9, 3),
			rows:     []ordermodel.OrderDailyPoint{row(2026, 9, 2, 3, 2, 1500)},
			wantDays: []string{"2026-09-02"},
			wantNet:  []int64{1500},
		},
		{
			name: "跨月：9-29 到 10-02 连续四天",
			from: day(2026, 9, 29),
			to:   day(2026, 10, 2),
			rows: []ordermodel.OrderDailyPoint{row(2026, 9, 30, 1, 1, 100), row(2026, 10, 1, 2, 2, 250)},
			// 9-29 与 10-02 没有订单（10-02 是半开上界对应的那天，不在区间内）。
			wantDays: []string{"2026-09-29", "2026-09-30", "2026-10-01"},
			wantNet:  []int64{0, 100, 250},
		},
		{
			name: "区间外的行既不上屏也不挤掉位置",
			from: day(2026, 9, 2),
			to:   day(2026, 9, 4),
			rows: []ordermodel.OrderDailyPoint{row(2026, 9, 1, 9, 9, 9999), row(2026, 9, 2, 1, 1, 100)},
			// 9-01 在 from 之前（模型层不会返回，但补零逻辑不能因此错位）。
			wantDays: []string{"2026-09-02", "2026-09-03"},
			wantNet:  []int64{100, 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			points := fillDailyPoints(tc.from, tc.to, tc.rows)
			if len(points) != len(tc.wantDays) {
				t.Fatalf("点数 = %d，期望 %d：%+v", len(points), len(tc.wantDays), points)
			}
			for i, want := range tc.wantDays {
				if points[i].Day != want {
					t.Errorf("第 %d 个点的日期 = %q，期望 %q", i, points[i].Day, want)
				}
				if points[i].NetSales != tc.wantNet[i] {
					t.Errorf("%s 的净销售额 = %d，期望 %d", want, points[i].NetSales, tc.wantNet[i])
				}
			}
		})
	}
}
