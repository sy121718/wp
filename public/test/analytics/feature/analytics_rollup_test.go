package feature

// analytics_rollup_test.go — 按天预聚合（审计 DB-005 / IDX-010）的接口链路测试。
//
// 覆盖点：
//   - 汇总数字与明细口径**逐项一致**（PV / UV / 按天 / 按路径）；
//   - 汇总幂等（同一天重算多少次，结果都等于明细算一遍）；
//   - 窗口含今天时**不读汇总**（今天的汇总行是快照，读它会让「刚发的文章没人看」看起来像故障）；
//   - 路径排行的游标分页与一次性取数结果一致（无重复、无遗漏）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
)

// seedView 直插一条访问明细（指定时间以便构造历史窗口）。
func seedView(t *testing.T, f *analyticsFixture, path, visitor string, at time.Time) {
	t.Helper()
	if err := f.db.Exec(
		`INSERT INTO page_views (project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at)
		 VALUES (?, ?, 'zh-CN', 'sess', ?, '', 'desktop', 'iphash', ?)`,
		f.projectID, path, visitor, at).Error; err != nil {
		t.Fatalf("插入访问明细失败: %v", err)
	}
}

// runRollup 触发一次汇总（运维/调度能力不进对外契约，走类型断言）。
func runRollup(t *testing.T, svc analyticscontract.AnalyticsService) int {
	t.Helper()
	roller, ok := svc.(interface {
		RollupRecent(context.Context) (int, error)
	})
	if !ok {
		t.Fatal("统计服务未提供按天汇总入口")
	}
	n, err := roller.RollupRecent(context.Background())
	if err != nil {
		t.Fatalf("汇总失败: %v", err)
	}
	return n
}

// utcDay 取某个时刻所在 UTC 日的零点（与模块内 dayStart 同口径）。
func utcDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func dayStr(t time.Time) string { return t.Format("2006-01-02") }

// summaryOf 查询一个窗口的统计结果。
func summaryOf(t *testing.T, f *analyticsFixture, from, to string) *analyticsdto.SummaryResp {
	t.Helper()
	res, err := f.svc.Summary(context.Background(), &analyticsdto.SummaryReq{
		ProjectID: f.projectID, From: from, To: to,
	})
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	return res
}

// TestRollupMatchesDetail 汇总与明细口径一致（DB-005 的验收条件）。
func TestRollupMatchesDetail(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	day1 := today.AddDate(0, 0, -3)
	day2 := today.AddDate(0, 0, -1)

	// day1：/a 两次（两个访客）、/b 一次（与 /a 共用访客 v1）；day2：/a 一次（第三个访客）。
	seedView(t, f, "/a", "v1", day1.Add(2*time.Hour))
	seedView(t, f, "/a", "v2", day1.Add(3*time.Hour))
	seedView(t, f, "/b", "v1", day1.Add(4*time.Hour))
	seedView(t, f, "/a", "v3", day2.Add(2*time.Hour))
	runRollup(t, f.svc)

	res := summaryOf(t, f, dayStr(day1), dayStr(day2))
	if res.Source != analyticsdto.SourceSummary {
		t.Fatalf("完全落在过去的窗口应读预聚合表，实际来源 %q", res.Source)
	}
	if res.Total != 4 || res.Visitors != 3 {
		t.Fatalf("窗口合计应为 PV=4 UV=3，实际 PV=%d UV=%d", res.Total, res.Visitors)
	}
	if len(res.Daily) != 2 {
		t.Fatalf("应有两天的按天数据: %+v", res.Daily)
	}
	if res.Daily[0].Day != dayStr(day1) || res.Daily[0].Views != 3 || res.Daily[0].Visitors != 2 {
		t.Fatalf("day1 应为 PV=3 UV=2: %+v", res.Daily[0])
	}
	if res.Daily[1].Day != dayStr(day2) || res.Daily[1].Views != 1 || res.Daily[1].Visitors != 1 {
		t.Fatalf("day2 应为 PV=1 UV=1: %+v", res.Daily[1])
	}
	// 路径排行：/a 3 次 3 个访客、/b 1 次 1 个访客（降序）。
	if len(res.Paths) != 2 {
		t.Fatalf("应有两条路径: %+v", res.Paths)
	}
	if res.Paths[0].Path != "/a" || res.Paths[0].Views != 3 || res.Paths[0].Visitors != 3 {
		t.Fatalf("路径排行第一条应为 /a PV=3 UV=3: %+v", res.Paths[0])
	}
	if res.Paths[1].Path != "/b" || res.Paths[1].Views != 1 {
		t.Fatalf("路径排行第二条应为 /b PV=1: %+v", res.Paths[1])
	}
	if res.PathTotal != 2 {
		t.Fatalf("路径总数应为 2: %d", res.PathTotal)
	}
	if res.PathNextAfter != "/b" || res.PathNextAfterViews != 1 {
		t.Fatalf("下一页游标应为最后一行(/b,1): %q %d", res.PathNextAfter, res.PathNextAfterViews)
	}

	// 交叉验证：把窗口右端延到今天（含今天 → 走明细），同样的历史数据应给出同样的数字。
	detail := summaryOf(t, f, dayStr(day1), dayStr(today))
	if detail.Source != analyticsdto.SourceDetail {
		t.Fatalf("含今天的窗口应读明细表，实际来源 %q", detail.Source)
	}
	if detail.Total != res.Total || detail.Visitors != res.Visitors {
		t.Fatalf("汇总与明细口径不一致：汇总 PV=%d UV=%d，明细 PV=%d UV=%d",
			res.Total, res.Visitors, detail.Total, detail.Visitors)
	}
}

// TestRollupIsIdempotent 重复汇总不翻倍、不残留。
func TestRollupIsIdempotent(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	day1 := today.AddDate(0, 0, -2)
	seedView(t, f, "/a", "v1", day1.Add(time.Hour))
	seedView(t, f, "/a", "v2", day1.Add(2*time.Hour))
	seedView(t, f, "/b", "v1", day1.Add(3*time.Hour))

	if n := runRollup(t, f.svc); n == 0 {
		t.Fatalf("汇总应至少覆盖 1 个有访问明细的工程")
	}
	first := summaryOf(t, f, dayStr(day1), dayStr(day1))
	runRollup(t, f.svc)
	runRollup(t, f.svc)
	again := summaryOf(t, f, dayStr(day1), dayStr(day1))

	if first.Total != again.Total || first.Visitors != again.Visitors {
		t.Fatalf("重复汇总改变了数字：%d/%d → %d/%d", first.Total, first.Visitors, again.Total, again.Visitors)
	}
	// 幂等的判据是「同一 (天, 粒度, 路径) 不产生第二行」—— 重算走 ON CONFLICT 覆盖。
	// 不写死总行数：汇总表会为「已汇总但当天无访问」的日子留下 views=0 的水位行，
	// 那些行的数量取决于补齐范围（今天/昨天固定会有），与幂等性无关。
	const dupSQL = "SELECT COUNT(*) FROM (" +
		"  SELECT project_id, day, scope, path FROM page_views_daily WHERE project_id = ?" +
		"  GROUP BY project_id, day, scope, path HAVING COUNT(*) > 1" +
		") t"
	var dupes int64
	if err := f.db.Raw(dupSQL, f.projectID).Scan(&dupes).Error; err != nil {
		t.Fatalf("统计重复汇总行失败: %v", err)
	}
	if dupes != 0 {
		t.Fatalf("重复汇总产生了 %d 组重复行", dupes)
	}
}

// TestSummaryIgnoresSummaryWhenWindowTouchesToday 含今天的窗口不读汇总。
//
// 用例往汇总表里塞了一行「今天」的假数据（views=999）：含今天的查询若读了它，
// 运营会看到一个与实际访问无关的数字 —— 这正是「汇总只服务过去」这条边界的反面。
func TestSummaryIgnoresSummaryWhenWindowTouchesToday(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	seedView(t, f, "/live", "v1", today.Add(time.Hour))
	if err := f.db.Exec(
		`INSERT INTO page_views_daily (project_id, day, scope, path, views, visitors)
		 VALUES (?, CURRENT_DATE, 'all', '', 999, 999)`,
		f.projectID).Error; err != nil {
		t.Fatalf("插入汇总假数据失败: %v", err)
	}

	res := summaryOf(t, f, dayStr(today), dayStr(today))
	if res.Source != analyticsdto.SourceDetail {
		t.Fatalf("含今天的窗口应读明细: %q", res.Source)
	}
	if res.Total != 1 || res.Visitors != 1 {
		t.Fatalf("应只统计明细里的 1 次访问，实际 PV=%d UV=%d", res.Total, res.Visitors)
	}
}

// TestPathRankingKeysetPagination 游标分页与一次性取数一致（IDX-010）。
func TestPathRankingKeysetPagination(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	day1 := today.AddDate(0, 0, -1)
	// 5 条路径，访问数刻意有并列（/p2 与 /p3 都是 2 次）—— 并列正是 offset 分页
	// 最容易出错的地方（顺序不稳定会导致翻页重复或漏项）。
	for i, views := range []int{5, 2, 2, 1, 1} {
		for j := 0; j < views; j++ {
			seedView(t, f, fmt.Sprintf("/p%d", i+1), fmt.Sprintf("v%d-%d", i, j), day1.Add(time.Duration(j+1)*time.Hour))
		}
	}
	runRollup(t, f.svc)

	// 一次性取全部（作为基准）。
	all := summaryOf(t, f, dayStr(day1), dayStr(day1))
	if len(all.Paths) != 5 {
		t.Fatalf("应有 5 条路径: %+v", all.Paths)
	}

	// 游标分页逐页取（每页 2 条）。
	var paged []analyticsdto.PathCount
	cursorViews, cursorPath := int64(0), ""
	for page := 0; page < 5; page++ {
		req := &analyticsdto.SummaryReq{
			ProjectID: f.projectID, From: dayStr(day1), To: dayStr(day1),
			PathLimit: 2, PathAfterViews: cursorViews, PathAfter: cursorPath,
		}
		res, err := f.svc.Summary(context.Background(), req)
		if err != nil {
			t.Fatalf("第 %d 页查询失败: %v", page+1, err)
		}
		if len(res.Paths) == 0 {
			break
		}
		paged = append(paged, res.Paths...)
		cursorViews, cursorPath = res.PathNextAfterViews, res.PathNextAfter
	}
	if len(paged) != len(all.Paths) {
		t.Fatalf("游标分页取到的条数应与一次性取数一致: %d vs %d", len(paged), len(all.Paths))
	}
	for i := range all.Paths {
		if paged[i] != all.Paths[i] {
			t.Fatalf("第 %d 条不一致：游标分页 %+v，一次性 %+v", i, paged[i], all.Paths[i])
		}
	}
}
