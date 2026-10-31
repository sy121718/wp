package feature

import (
	"context"
	"fmt"
	"testing"
	"time"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
)

func seedView(t *testing.T, f *analyticsFixture, path, visitor string, at time.Time) {
	t.Helper()
	if err := f.db.Exec(
		`INSERT INTO page_views (project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at)
		 VALUES (?, ?, 'zh-CN', 'sess', ?, '', 'desktop', 'iphash', ?)`,
		f.projectID, path, visitor, at).Error; err != nil {
		t.Fatalf("插入访问明细失败: %v", err)
	}
}

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

func utcDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func dayStr(t time.Time) string { return t.Format("2006-01-02") }

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

func TestRollupMatchesDetail(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	day1 := today.AddDate(0, 0, -3)
	day2 := today.AddDate(0, 0, -1)

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

	detail := summaryOf(t, f, dayStr(day1), dayStr(today))
	if detail.Source != analyticsdto.SourceDetail {
		t.Fatalf("含今天的窗口应读明细表，实际来源 %q", detail.Source)
	}
	if detail.Total != res.Total || detail.Visitors != res.Visitors {
		t.Fatalf("汇总与明细口径不一致：汇总 PV=%d UV=%d，明细 PV=%d UV=%d",
			res.Total, res.Visitors, detail.Total, detail.Visitors)
	}
}

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

func TestPathRankingKeysetPagination(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	day1 := today.AddDate(0, 0, -1)

	for i, views := range []int{5, 2, 2, 1, 1} {
		for j := 0; j < views; j++ {
			seedView(t, f, fmt.Sprintf("/p%d", i+1), fmt.Sprintf("v%d-%d", i, j), day1.Add(time.Duration(j+1)*time.Hour))
		}
	}
	runRollup(t, f.svc)

	all := summaryOf(t, f, dayStr(day1), dayStr(day1))
	if len(all.Paths) != 5 {
		t.Fatalf("应有 5 条路径: %+v", all.Paths)
	}

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

func TestSummaryUVIsNotSumOfDailyUV(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	day1 := today.AddDate(0, 0, -3)
	day2 := today.AddDate(0, 0, -1)

	seedView(t, f, "/a", "v1", day1.Add(2*time.Hour))
	seedView(t, f, "/a", "v2", day1.Add(3*time.Hour))
	seedView(t, f, "/a", "v1", day2.Add(2*time.Hour))
	runRollup(t, f.svc)

	sum := summaryOf(t, f, dayStr(day1), dayStr(day2))
	if sum.Source != analyticsdto.SourceSummary {
		t.Fatalf("完全落在过去的窗口应读预聚合表，实际 %q", sum.Source)
	}

	if sum.Total != 3 {
		t.Fatalf("PV 应为 3，实际 %d", sum.Total)
	}
	if sum.Visitors != 2 {
		t.Fatalf("区间 UV 应为 2（v1 跨天只算一次），实际 %d —— "+
			"SUM(按天 UV) 把回访者重复计数，把「人次」冒充成「人数」", sum.Visitors)
	}

	if len(sum.Daily) != 2 {
		t.Fatalf("应有两天的按天数据: %+v", sum.Daily)
	}
	if sum.Daily[0].Visitors != 2 || sum.Daily[1].Visitors != 1 {
		t.Fatalf("按天 UV 应各自去重（day1=2, day2=1），实际 %d / %d",
			sum.Daily[0].Visitors, sum.Daily[1].Visitors)
	}

	detail := summaryOf(t, f, dayStr(day1), dayStr(today))
	if detail.Source != analyticsdto.SourceDetail {
		t.Fatalf("含今天的窗口应读明细表，实际 %q", detail.Source)
	}
	if detail.Visitors != sum.Visitors {
		t.Fatalf("汇总与明细的 UV 不一致：汇总 %d，明细 %d（同一批数据不该有两个答案）",
			sum.Visitors, detail.Visitors)
	}
}
