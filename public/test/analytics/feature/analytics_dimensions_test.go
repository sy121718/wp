package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsmodel "go_wp/internal/module/analytics/model"
)

type dimSeed struct {
	path     string
	visitor  string
	referrer string
	uaClass  string
	lang     string
	at       time.Time
}

func seedDim(t *testing.T, f *analyticsFixture, s dimSeed) {
	t.Helper()
	const sql = "INSERT INTO page_views " +
		"(project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at) " +
		"VALUES (?, ?, ?, 'sess', ?, ?, ?, 'iphash', ?)"
	if err := f.db.Exec(sql, f.projectID, s.path, s.lang, s.visitor, s.referrer, s.uaClass, s.at).Error; err != nil {
		t.Fatalf("插入访问明细失败: %v", err)
	}
}

func summaryRank(t *testing.T, f *analyticsFixture, from, to string, rankLimit int) *analyticsdto.SummaryResp {
	t.Helper()
	res, err := f.svc.Summary(context.Background(), &analyticsdto.SummaryReq{
		ProjectID: f.projectID, From: from, To: to, RankLimit: rankLimit,
	})
	if err != nil {
		t.Fatalf("查询统计失败: %v", err)
	}
	return res
}

func rankValues(rows []analyticsdto.RankCount) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Value)
	}
	return out
}

func rankViews(rows []analyticsdto.RankCount) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Views)
	}
	return out
}

func rankVisitors(rows []analyticsdto.RankCount) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Visitors)
	}
	return out
}

func equalInt64s(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertRanks(t *testing.T, name string, rows []analyticsdto.RankCount,
	wantValues []string, wantViews, wantVisitors []int64) {
	t.Helper()
	if got := rankValues(rows); !equalStrings(got, wantValues) {
		t.Fatalf("%s 取值/顺序不符：实际 %v，期望 %v", name, got, wantValues)
	}
	if got := rankViews(rows); !equalInt64s(got, wantViews) {
		t.Fatalf("%s 浏览数不符：实际 %v，期望 %v", name, got, wantViews)
	}
	if got := rankVisitors(rows); !equalInt64s(got, wantVisitors) {
		t.Fatalf("%s 独立访客不符：实际 %v，期望 %v", name, got, wantVisitors)
	}
}

func seedDimensionData(t *testing.T, f *analyticsFixture) time.Time {
	t.Helper()
	day := utcDay(time.Now()).AddDate(0, 0, -1)
	rows := []dimSeed{
		{visitor: "v1", referrer: "ref-a.example.com", uaClass: "desktop", lang: "zh-CN"},
		{visitor: "v2", referrer: "ref-a.example.com", uaClass: "desktop", lang: "zh-CN"},
		{visitor: "v1", referrer: "ref-b.example.com", uaClass: "mobile", lang: "zh-CN"},
		{visitor: "v3", referrer: "", uaClass: "mobile", lang: "en"},
		{visitor: "v3", referrer: "", uaClass: "tablet", lang: "en"},
		{visitor: "v4", referrer: "", uaClass: "bot", lang: ""},
		{visitor: "v5", referrer: "ref-a.example.com", uaClass: "", lang: "zh-CN"},
		{visitor: "v1", referrer: "ref-c.example.com", uaClass: "desktop", lang: ""},
	}
	for i, s := range rows {
		s.path = "/a"
		s.at = day.Add(time.Duration(i+1) * time.Hour)
		seedDim(t, f, s)
	}
	return day
}

func TestAnalyticsDimensionRankingAggregates(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := seedDimensionData(t, f)
	res := summaryRank(t, f, dayStr(day), dayStr(day), 0)

	if res.Source != analyticsdto.SourceDetail {
		t.Fatalf("未汇总的窗口应读明细，实际来源 %q", res.Source)
	}

	assertRanks(t, "来源域", res.Referrers,
		[]string{"", "ref-a.example.com", "ref-b.example.com", "ref-c.example.com"},
		[]int64{3, 3, 1, 1}, []int64{2, 3, 1, 1})

	assertRanks(t, "设备分类", res.UAClasses,
		[]string{"desktop", "mobile", "", "bot", "tablet"},
		[]int64{3, 2, 1, 1, 1}, []int64{2, 2, 1, 1, 1})

	assertRanks(t, "语言", res.Langs,
		[]string{"zh-CN", "", "en"},
		[]int64{4, 2, 2}, []int64{3, 2, 1})

	for name, rows := range map[string][]analyticsdto.RankCount{
		"来源域": res.Referrers, "设备分类": res.UAClasses, "语言": res.Langs,
	} {
		var sum int64
		for _, r := range rows {
			sum += r.Views
		}
		if sum != res.Total {
			t.Errorf("%s 的浏览数之和 %d 与总 PV %d 不等（空值被过滤掉了？）", name, sum, res.Total)
		}
	}
	if res.BreakdownSource != analyticsdto.SourceDetail {
		t.Errorf("维度排行的来源应恒为 detail，实际 %q", res.BreakdownSource)
	}
	if res.RankLimit != 20 {
		t.Errorf("未指定 rankLimit 时应回显默认值 20，实际 %d", res.RankLimit)
	}
}

func TestAnalyticsDimensionTieBreakIsDeterministic(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := utcDay(time.Now()).AddDate(0, 0, -1)
	for i := 20; i >= 1; i-- {
		seedDim(t, f, dimSeed{
			path: "/a", visitor: fmt.Sprintf("v%02d", i),
			referrer: fmt.Sprintf("r%02d.example.com", i), uaClass: "desktop", lang: "zh-CN",
			at: day.Add(time.Duration(i) * time.Minute),
		})
	}
	const limit = 8
	want := []string{
		"r01.example.com", "r02.example.com", "r03.example.com", "r04.example.com",
		"r05.example.com", "r06.example.com", "r07.example.com", "r08.example.com",
	}

	first := summaryRank(t, f, dayStr(day), dayStr(day), limit).Referrers
	if len(first) != limit {
		t.Fatalf("应返回 %d 条，实际 %d 条: %v", limit, len(first), rankValues(first))
	}
	if got := rankValues(first); !equalStrings(got, want) {
		t.Fatalf("并列时的次序不确定（应取字典序最小的一批）：实际 %v，期望 %v", got, want)
	}

	for i := 0; i < 3; i++ {
		again := summaryRank(t, f, dayStr(day), dayStr(day), limit).Referrers
		if got := rankValues(again); !equalStrings(got, want) {
			t.Fatalf("第 %d 次重复查询的榜变了：%v", i+1, got)
		}
	}

	all := summaryRank(t, f, dayStr(day), dayStr(day), maxRankRequest).Referrers
	if len(all) != 20 {
		t.Fatalf("不限条数时应返回全部 20 条，实际 %d 条", len(all))
	}
	if got := rankValues(all)[:limit]; !equalStrings(got, want) {
		t.Fatalf("截断查询与全量查询的前 %d 条不一致：%v", limit, got)
	}
}

const maxRankRequest = 100000

func TestAnalyticsDimensionRankingAcrossWindowSwitch(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := seedDimensionData(t, f)
	runRollup(t, f.svc)

	past := summaryRank(t, f, dayStr(day), dayStr(day), 0)
	if past.Source != analyticsdto.SourceSummary {
		t.Fatalf("完全落在过去且已汇总的窗口应读预聚合，实际来源 %q", past.Source)
	}
	live := summaryRank(t, f, dayStr(day), dayStr(utcDay(time.Now())), 0)
	if live.Source != analyticsdto.SourceDetail {
		t.Fatalf("含今天的窗口应读明细，实际来源 %q", live.Source)
	}

	names := []string{"来源域", "设备分类", "语言"}
	pairs := [][2][]analyticsdto.RankCount{
		{past.Referrers, live.Referrers},
		{past.UAClasses, live.UAClasses},
		{past.Langs, live.Langs},
	}
	for i, name := range names {
		if len(pairs[i][0]) == 0 {
			t.Fatalf("%s 在走预聚合的窗口上不该是空榜", name)
		}
		if !equalStrings(rankValues(pairs[i][0]), rankValues(pairs[i][1])) ||
			!equalInt64s(rankViews(pairs[i][0]), rankViews(pairs[i][1])) {
			t.Errorf("%s 两种取数分支结果不一致：预聚合窗口 %v，明细窗口 %v",
				name, rankValues(pairs[i][0]), rankValues(pairs[i][1]))
		}
	}
	if past.BreakdownSource != analyticsdto.SourceDetail || live.BreakdownSource != analyticsdto.SourceDetail {
		t.Errorf("维度排行的取数来源应恒为 detail：past=%q live=%q", past.BreakdownSource, live.BreakdownSource)
	}
}

func TestAnalyticsDimensionIsolationAndLimits(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := utcDay(time.Now()).AddDate(0, 0, -1)
	seedDim(t, f, dimSeed{
		path: "/a", visitor: "v1", referrer: "mine.example.com",
		uaClass: "desktop", lang: "zh-CN", at: day.Add(time.Hour),
	})

	const otherProject = "2f1a4c0e-0000-4000-8000-0000000000ff"
	const foreignSQL = "INSERT INTO page_views " +
		"(project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at) " +
		"VALUES (?, '/a', 'zh-CN', 'sess', 'v9', 'foreign.example.com', 'desktop', 'iphash', ?)"
	if err := f.db.Exec(foreignSQL, otherProject, day.Add(2*time.Hour)).Error; err != nil {
		t.Fatalf("插入其他工程的访问失败: %v", err)
	}

	res := summaryRank(t, f, dayStr(day), dayStr(day), 0)
	assertRanks(t, "来源域（工程隔离）", res.Referrers,
		[]string{"mine.example.com"}, []int64{1}, []int64{1})

	capped := summaryRank(t, f, dayStr(day), dayStr(day), maxRankRequest)
	if capped.RankLimit != 200 {
		t.Errorf("rankLimit 超上限应收敛到 200，实际 %d", capped.RankLimit)
	}
	if len(capped.Referrers) != 1 {
		t.Errorf("收敛后的榜应仍返回实际存在的 1 条，实际 %d 条", len(capped.Referrers))
	}

	m := analyticsmodel.NewModel(f.db)
	const injection = "referrer_host) FROM page_views; DROP TABLE page_views; --"
	if _, err := m.CountByDimension(context.Background(), f.projectID, day, day.AddDate(0, 0, 1), injection, 10); err == nil {
		t.Fatal("未知维度应被拒绝，而不是拼进 SQL")
	}
}

func TestAnalyticsDimensionEmptyProject(t *testing.T) {
	f := newAnalyticsFixture(t)
	res, err := f.svc.Summary(context.Background(), &analyticsdto.SummaryReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("空工程查询失败: %v", err)
	}
	if res.Referrers == nil || res.UAClasses == nil || res.Langs == nil {
		t.Fatalf("空工程的三组榜应是空切片（JSON 的 []）而不是 nil（null）: %+v", res)
	}
	if n := len(res.Referrers) + len(res.UAClasses) + len(res.Langs); n != 0 {
		t.Fatalf("空工程不该有任何维度的行，实际 %d 条", n)
	}
	if res.BreakdownSource != analyticsdto.SourceDetail {
		t.Errorf("空表下取数来源仍应回显 detail，实际 %q", res.BreakdownSource)
	}

	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, summaryPath+"?projectId="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("只读接口应回 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Referrers       []map[string]any `json:"referrers"`
			UAClasses       []map[string]any `json:"uaClasses"`
			Langs           []map[string]any `json:"langs"`
			RankLimit       int              `json:"rankLimit"`
			BreakdownSource string           `json:"breakdownSource"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v（body=%s）", err, rec.Body.String())
	}
	if body.Data.Referrers == nil || body.Data.UAClasses == nil || body.Data.Langs == nil {
		t.Errorf("JSON 里三组榜应是 [] 而不是 null：%s", rec.Body.String())
	}
	if body.Data.RankLimit != 20 || body.Data.BreakdownSource != analyticsdto.SourceDetail {
		t.Errorf("rankLimit / breakdownSource 回显不符：%d / %q",
			body.Data.RankLimit, body.Data.BreakdownSource)
	}
}

func TestAnalyticsDimensionLargeWindow(t *testing.T) {
	f := newAnalyticsFixture(t)
	today := utcDay(time.Now())
	for d := 7; d >= 0; d-- {
		day := today.AddDate(0, 0, -d)
		referrer := "ref-even.example.com"
		if d%2 == 1 {
			referrer = "ref-odd.example.com"
		}
		seedDim(t, f, dimSeed{
			path: "/a", visitor: fmt.Sprintf("v%d-a", d), referrer: referrer,
			uaClass: "desktop", lang: "zh-CN", at: day.Add(time.Hour),
		})
		seedDim(t, f, dimSeed{
			path: "/a", visitor: fmt.Sprintf("v%d-b", d), referrer: referrer,
			uaClass: "mobile", lang: "en", at: day.Add(2 * time.Hour),
		})
	}
	res := summaryRank(t, f, dayStr(today.AddDate(0, 0, -7)), dayStr(today), 0)
	if res.Total != 16 {
		t.Fatalf("窗口内应有 16 次访问，实际 %d", res.Total)
	}

	assertRanks(t, "来源域（大窗口）", res.Referrers,
		[]string{"ref-even.example.com", "ref-odd.example.com"}, []int64{8, 8}, []int64{8, 8})
	assertRanks(t, "设备分类（大窗口）", res.UAClasses,
		[]string{"desktop", "mobile"}, []int64{8, 8}, []int64{8, 8})
	assertRanks(t, "语言（大窗口）", res.Langs,
		[]string{"en", "zh-CN"}, []int64{8, 8}, []int64{8, 8})
}

func TestAnalyticsDimensionUnavailableAfterRetentionPurge(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := utcDay(time.Now()).AddDate(0, 0, -1)

	const rollupSQL = "INSERT INTO page_views_daily (project_id, day, scope, path, views, visitors, rolled_at) " +
		"VALUES (?, ?::date, ?, ?, ?, ?, now())"
	if err := f.db.Exec(rollupSQL, f.projectID, dayStr(day), "all", "", 7, 4).Error; err != nil {
		t.Fatalf("插入汇总行失败: %v", err)
	}
	if err := f.db.Exec(rollupSQL, f.projectID, dayStr(day), "path", "/a", 7, 4).Error; err != nil {
		t.Fatalf("插入路径汇总行失败: %v", err)
	}

	res := summaryRank(t, f, dayStr(day), dayStr(day), 0)
	if res.Source != analyticsdto.SourceSummary {
		t.Fatalf("已汇总的过去窗口应走预聚合，实际来源 %q", res.Source)
	}
	if res.Total != 7 || res.Visitors != 4 {
		t.Fatalf("总数应来自汇总行（PV=7 UV=4），实际 PV=%d UV=%d", res.Total, res.Visitors)
	}
	if len(res.Paths) != 1 || res.Paths[0].Path != "/a" {
		t.Fatalf("路径榜读汇总，应有那条 /a 行，实际 %+v", res.Paths)
	}
	if n := len(res.Referrers) + len(res.UAClasses) + len(res.Langs); n != 0 {
		t.Fatalf("明细为空时三个维度榜都应为空，实际 ref=%d ua=%d lang=%d",
			len(res.Referrers), len(res.UAClasses), len(res.Langs))
	}

	if res.Total <= 0 || res.BreakdownSource != analyticsdto.SourceDetail {
		t.Fatalf("判据输入不符：total=%d breakdownSource=%q", res.Total, res.BreakdownSource)
	}
}
