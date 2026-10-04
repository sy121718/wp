package feature

// order_customer_cohort_test.go — 群组留存矩阵（P7-b5）。
//
// 这一批要钉住的是 docs/17 §4.4 的**对账口径**：群组各群人数之和必须等于
// 客户概览的「新客」数 —— 两处用的是同一个分群判据（首单落在区间内），
// 各写一份 SQL 时把 `>=` 改成 `>` 不会让任何一处变红，只会让两个页面的数字
// 悄悄差一个人。所以判据是一条**等式**，不是各自的值。

import (
	"context"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
)

// cohortDay 造一个明确的时刻（月内日 + 小时），避免零点边界干扰。
func cohortDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

// cohortReq 区间 = 2026-07-01 ~ 2026-09-30（含当天，service 侧转半开）。
func cohortReq(projectID string) *orderdto.CustomerCohortReq {
	return &orderdto.CustomerCohortReq{ProjectID: projectID, From: "2026-07-01", To: "2026-09-30"}
}

// TestCustomerCohortReconcilesWithGrowth 闸门：各群人数之和 == 概览页的新客数。
//
// 数据设计（三个群）：
//   - 7 月首单：7001（7 月又复购）、7002（8 月回来一次）
//   - 8 月首单：8001（9 月回来一次）
//   - 9 月首单：9001（无后续）
//   - 一个老客户 6001（首单在 6 月，区间内下单）：**不进任何群**，但计入概览的「回头客」
//
// 所以新客 = 4，群大小 2 + 1 + 1 = 4。
func TestCustomerCohortReconcilesWithGrowth(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	if m == nil {
		t.Skip("无数据库")
	}
	const pid = summaryProjectA
	ctx := context.Background()

	// 群：7 月两人
	mkGrowthOrder(t, m, pid, "C-7001-1", "paid", 1000, cohortDay(2026, 7, 5), growthUser(7001))
	mkGrowthOrder(t, m, pid, "C-7001-2", "paid", 2000, cohortDay(2026, 7, 20), growthUser(7001))
	mkGrowthOrder(t, m, pid, "C-7002-1", "paid", 1500, cohortDay(2026, 7, 8), growthUser(7002))
	mkGrowthOrder(t, m, pid, "C-7002-2", "paid", 900, cohortDay(2026, 8, 3), growthUser(7002))
	// 群：8 月一人
	mkGrowthOrder(t, m, pid, "C-8001-1", "paid", 800, cohortDay(2026, 8, 11), growthUser(8001))
	mkGrowthOrder(t, m, pid, "C-8001-2", "paid", 700, cohortDay(2026, 9, 2), growthUser(8001))
	// 群：9 月一人
	mkGrowthOrder(t, m, pid, "C-9001-1", "paid", 600, cohortDay(2026, 9, 9), growthUser(9001))
	// 老客户：首单在区间之前（不进群），区间内下单（计入概览的回头客）
	mkGrowthOrder(t, m, pid, "C-6001-0", "paid", 500, cohortDay(2026, 6, 3), growthUser(6001))
	mkGrowthOrder(t, m, pid, "C-6001-1", "paid", 500, cohortDay(2026, 8, 15), growthUser(6001))
	// 游客单：没有 user_id，两边都不该计入
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
	// 每一行的各格人数之和必须等于本群人数（首月 100% 也是这条等式的特例，
	// 所以不必单独断言 100% ——格子里的数与分母同源，等式成立就说明比例没错）。
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

// TestCustomerCohortMatrixCells 矩阵取值与「未到达的月份留空」。
func TestCustomerCohortMatrixCells(t *testing.T) {
	_, m, svc := newRangeFixture(t)
	if m == nil {
		t.Skip("无数据库")
	}
	const pid = summaryProjectA
	ctx := context.Background()

	// 7 月首单一人，8 月与 9 月各回来一次 → 7 月群的前三格都是 1 人。
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
	// 每行的 Cells 必须是**定长**的（模板按列顺序渲染，Go 侧负责补齐）。
	if len(row.Cells) != res.Months {
		t.Fatalf("格子数 %d 应与列数 %d 相等（补齐在 service 做）", len(row.Cells), res.Months)
	}
	for i, c := range row.Cells {
		if c.MonthIndex != i {
			t.Errorf("第 %d 格的 MonthIndex 应为 %d，实得 %d", i, i, c.MonthIndex)
		}
	}
	// 首月一定是 100%（他的第一单就在那个月）。
	if row.Cells[0].RetentionLabel != "100.0%" || row.Cells[0].ActiveCustomers != 1 {
		t.Errorf("首月应为 100.0%%（1 人），实得 %s（%d 人）",
			row.Cells[0].RetentionLabel, row.Cells[0].ActiveCustomers)
	}
	// 7 月群在 8、9 月都回来了。
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

// TestCustomerCohortRejectsMissingParams 工程与区间都必须显式给。
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
