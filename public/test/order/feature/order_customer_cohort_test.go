package feature

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

func cohortDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

func cohortReq(projectID string) *orderdto.CustomerCohortReq {
	return &orderdto.CustomerCohortReq{ProjectID: projectID, From: "2026-07-01", To: "2026-09-30"}
}

func TestCustomerCohortReconcilesWithGrowth(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	if m == nil {
		t.Skip("无数据库")
	}
	const pid = summaryProjectA
	ctx := context.Background()

	mkGrowthOrder(t, m, pid, "C-7001-1", "paid", 1000, cohortDay(2026, 7, 5), growthUser(7001))
	mkGrowthOrder(t, m, pid, "C-7001-2", "paid", 2000, cohortDay(2026, 7, 20), growthUser(7001))
	mkGrowthOrder(t, m, pid, "C-7002-1", "paid", 1500, cohortDay(2026, 7, 8), growthUser(7002))
	mkGrowthOrder(t, m, pid, "C-7002-2", "paid", 900, cohortDay(2026, 8, 3), growthUser(7002))

	mkGrowthOrder(t, m, pid, "C-8001-1", "paid", 800, cohortDay(2026, 8, 11), growthUser(8001))
	mkGrowthOrder(t, m, pid, "C-8001-2", "paid", 700, cohortDay(2026, 9, 2), growthUser(8001))

	mkGrowthOrder(t, m, pid, "C-9001-1", "paid", 600, cohortDay(2026, 9, 9), growthUser(9001))

	mkGrowthOrder(t, m, pid, "C-6001-0", "paid", 500, cohortDay(2026, 6, 3), growthUser(6001))
	mkGrowthOrder(t, m, pid, "C-6001-1", "paid", 500, cohortDay(2026, 8, 15), growthUser(6001))

	mkGrowthOrder(t, m, pid, "C-guest", "paid", 300, cohortDay(2026, 7, 12), nil)

	res, err := svc.CustomerCohortByRange(ctx, cohortReq(pid))
	if err != nil {
		t.Fatalf("取群组留存失败: %v", err)
	}
	growth, err := svc.CustomerGrowthByRange(ctx, &orderdto.CustomerGrowthReq{
		ProjectID: pid, From: "2026-07-01", To: "2026-09-30",
	})
	if err != nil {
		t.Fatalf("取客户增长失败: %v", err)
	}

	if res.Customers != growth.NewCustomers {
		t.Errorf("对账失败：群组人数合计 %d != 概览新客 %d（同一判据，两处必须相等）",
			res.Customers, growth.NewCustomers)
	}
	if growth.NewCustomers != 4 {
		t.Fatalf("前置数据不符：新客应为 4，实得 %d", growth.NewCustomers)
	}
	if growth.ReturningCustomers != 1 {
		t.Errorf("区间内的 6 月老客户应计入回头客 1，实得 %d", growth.ReturningCustomers)
	}
	if res.Cohorts != 3 {
		t.Errorf("应有三群（7/8/9 月），实得 %d", res.Cohorts)
	}

	for _, row := range res.Rows {
		var active int64
		for _, c := range row.Cells {
			active += c.ActiveCustomers
		}
		if active != row.CohortSize && row.Cells[0].ActiveCustomers != row.CohortSize {
			t.Errorf("群 %s：各格合计 %d 与本群人数 %d 不符", row.CohortMonth, active, row.CohortSize)
		}
	}
}

func TestCustomerCohortMatrixCells(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	if m == nil {
		t.Skip("无数据库")
	}
	const pid = summaryProjectA
	ctx := context.Background()

	mkGrowthOrder(t, m, pid, "M-1", "paid", 1000, cohortDay(2026, 7, 4), growthUser(7101))
	mkGrowthOrder(t, m, pid, "M-2", "paid", 1000, cohortDay(2026, 8, 4), growthUser(7101))
	mkGrowthOrder(t, m, pid, "M-3", "paid", 1000, cohortDay(2026, 9, 4), growthUser(7101))

	res, err := svc.CustomerCohortByRange(ctx, cohortReq(pid))
	if err != nil {
		t.Fatalf("取群组留存失败: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("应只有一群，实得 %d", len(res.Rows))
	}
	row := res.Rows[0]
	if row.CohortSize != 1 {
		t.Fatalf("群人数应为 1，实得 %d", row.CohortSize)
	}

	if len(row.Cells) != res.Months {
		t.Fatalf("格子数 %d 应与列数 %d 相等（补齐在 service 做）", len(row.Cells), res.Months)
	}
	for i, c := range row.Cells {
		if c.MonthIndex != i {
			t.Errorf("第 %d 格的 MonthIndex 应为 %d，实得 %d", i, i, c.MonthIndex)
		}
	}

	if row.Cells[0].RetentionLabel != "100.0%" || row.Cells[0].ActiveCustomers != 1 {
		t.Errorf("首月应为 100.0%%（1 人），实得 %s（%d 人）",
			row.Cells[0].RetentionLabel, row.Cells[0].ActiveCustomers)
	}

	for _, idx := range []int{1, 2} {
		if idx >= len(row.Cells) {
			t.Fatalf("列数不足：应有 %d 列（7/8/9 月），实得 %d", 3, res.Months)
		}
		if row.Cells[idx].ActiveCustomers != 1 || !row.Cells[idx].Reached {
			t.Errorf("第 %d 格应已到达且有 1 人，实得 reached=%v active=%d",
				idx, row.Cells[idx].Reached, row.Cells[idx].ActiveCustomers)
		}
	}
}

func TestCustomerCohortRejectsMissingParams(t *testing.T) {
	_, _, svc := newRangeFixture(t)
	if svc == nil {
		t.Skip("无数据库")
	}
	ctx := context.Background()
	for name, req := range map[string]*orderdto.CustomerCohortReq{
		"缺工程": {From: "2026-07-01", To: "2026-09-30"},
		"缺区间": {ProjectID: "p1"},
		"空请求": nil,
	} {
		if _, err := svc.CustomerCohortByRange(ctx, req); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
}
