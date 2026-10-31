package orderservice

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

			wantDays: []string{"2026-09-29", "2026-09-30", "2026-10-01"},
			wantNet:  []int64{0, 100, 250},
		},
		{
			name: "区间外的行既不上屏也不挤掉位置",
			from: day(2026, 9, 2),
			to:   day(2026, 9, 4),
			rows: []ordermodel.OrderDailyPoint{row(2026, 9, 1, 9, 9, 9999), row(2026, 9, 2, 1, 1, 100)},

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

func TestFillHourlyPointsNormalizesBucketsToUTC(t *testing.T) {
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	cst := time.FixedZone("CST", 8*3600)

	bucket := time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC)
	rows := []ordermodel.OrderDailyPoint{{Day: bucket.In(cst), OrderCount: 7, NetSales: 1200}}

	points := fillHourlyPoints(from, to, rows)
	if len(points) != 24 {
		t.Fatalf("按小时应得到 24 个点，实得 %d", len(points))
	}
	if points[0].Day != "2026-10-05T00:00" || points[23].Day != "2026-10-05T23:00" {
		t.Errorf("桶键首尾 = %q / %q，期望 2026-10-05T00:00 / 2026-10-05T23:00", points[0].Day, points[23].Day)
	}
	got := int64(-1)
	for _, p := range points {
		if p.Day == "2026-10-05T05:00" {
			got = p.OrderCount
		}
	}
	if got != 7 {
		t.Errorf("05:00 桶的订单数 = %d，期望 7（行上是本地位置的同一时刻，归一后应命中）", got)
	}

	for _, p := range points {
		if len(p.Day) != 16 || p.Day[:10] != "2026-10-05" {
			t.Errorf("桶键 %q 不在 2026-10-05（时区没归一时会漂到前一天）", p.Day)
		}
	}
}
