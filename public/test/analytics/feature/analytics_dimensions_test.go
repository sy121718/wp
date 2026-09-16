package feature

// analytics_dimensions_test.go — 来源域 / 设备分类 / 语言三个维度的读路径。
//
// 背景：这三个维度的值从打点第一天起就在落库（referrer_host / ua_class / lang），
// 但一直没有读取方 —— 存储与写入成本付了，洞察拿不到。本文件覆盖补上的读路径。
//
// 覆盖点：
//   - 三组排行的聚合正确性（views / visitors / 顺序）；
//   - 并列排序的**确定性**（同 views 按取值升序，Top-N 的截断点因此可复现）；
//   - 空值不被静默丢弃（空值参与排行，各榜 views 之和等于窗口总 PV）；
//   - 窗口切换（走明细与走预聚合两种分支下，维度排行给出同一份结果）；
//   - 工程隔离、条数归一化、未知维度被白名单拒绝。

import (
	"context"
	"fmt"
	"testing"
	"time"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsmodel "go_wp/internal/module/analytics/model"
)

// dimSeed 一条带完整维度字段的访问明细。
type dimSeed struct {
	path     string
	visitor  string
	referrer string
	uaClass  string
	lang     string
	at       time.Time
}

// seedDim 直插一条访问明细（维度字段与时间显式指定，便于构造窗口与并列）。
func seedDim(t *testing.T, f *analyticsFixture, s dimSeed) {
	t.Helper()
	const sql = "INSERT INTO page_views " +
		"(project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at) " +
		"VALUES (?, ?, ?, 'sess', ?, ?, ?, 'iphash', ?)"
	if err := f.db.Exec(sql, f.projectID, s.path, s.lang, s.visitor, s.referrer, s.uaClass, s.at).Error; err != nil {
		t.Fatalf("插入访问明细失败: %v", err)
	}
}

// summaryRank 按窗口 + 条数上限查询统计（RankLimit 是本次新增的入参）。
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

// rankValues / rankViews / rankVisitors 把排行拍平成可比较的切片（断言用）。
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

// equalInt64s / equalStrings 逐项比较。
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

// assertRanks 断言一组排行的取值 / 浏览数 / 独立访客逐项相等。
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

// seedDimensionData 构造固定数据集（8 次访问）并返回数据所在的 UTC 日（昨天）。
//
// 清单（referrer, ua_class, lang, visitor）：
//  1. ref-a.example.com, desktop, zh-CN, v1
//  2. ref-a.example.com, desktop, zh-CN, v2
//  3. ref-b.example.com, mobile,  zh-CN, v1
//  4. "",                mobile,  en,    v3
//  5. "",                tablet,  en,    v3
//  6. "",                bot,     "",    v4
//  7. ref-a.example.com, "",      zh-CN, v5
//  8. ref-c.example.com, desktop, "",    v1
//
// 每个维度都同时含**并列**（views 相同的取值）与**空值**，两种断言共用一份数据。
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

// TestAnalyticsDimensionRankingAggregates 三组排行的聚合、并列顺序与空值保留。
func TestAnalyticsDimensionRankingAggregates(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := seedDimensionData(t, f)
	res := summaryRank(t, f, dayStr(day), dayStr(day), 0)

	if res.Source != analyticsdto.SourceDetail {
		t.Fatalf("未汇总的窗口应读明细，实际来源 %q", res.Source)
	}
	// 来源域：ref-a 与「无来源」并列 3 次 —— 空串在 PG 排序里最小，所以它在并列块里排前。
	assertRanks(t, "来源域", res.Referrers,
		[]string{"", "ref-a.example.com", "ref-b.example.com", "ref-c.example.com"},
		[]int64{3, 3, 1, 1}, []int64{2, 3, 1, 1})
	// 设备分类：desktop 3 / mobile 2 / 其余三个都是 1（空串、bot、tablet 按取值升序）。
	assertRanks(t, "设备分类", res.UAClasses,
		[]string{"desktop", "mobile", "", "bot", "tablet"},
		[]int64{3, 2, 1, 1, 1}, []int64{2, 2, 1, 1, 1})
	// 语言：zh-CN 4 / en 与空串并列 2（空串在前）。
	assertRanks(t, "语言", res.Langs,
		[]string{"zh-CN", "", "en"},
		[]int64{4, 2, 2}, []int64{3, 2, 1})

	// 空值没有被静默丢掉：三组榜的浏览数之和都等于窗口总 PV（8）。
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

// TestAnalyticsDimensionTieBreakIsDeterministic 并列排序确定：Top-N 的截断点可复现。
//
// 20 个来源各 1 次访问（views 全部并列），且**插入顺序与字典序相反**（r20 → r01）：
// 只按 views 排序时，取前 8 条由执行计划决定，字典序最小的一批不保证入选 ——
// 榜尾的名次会在两次刷新之间漂移。次级键（取值升序）是这条断言成立的前提。
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
	// 重复查询必须给出同一份榜 —— 顺序不稳定时这里会随机失败。
	for i := 0; i < 3; i++ {
		again := summaryRank(t, f, dayStr(day), dayStr(day), limit).Referrers
		if got := rankValues(again); !equalStrings(got, want) {
			t.Fatalf("第 %d 次重复查询的榜变了：%v", i+1, got)
		}
	}
	// 不截断时是全部 20 条，且前 8 条与截断查询一致 —— 说明 limit 只截尾，不换序。
	all := summaryRank(t, f, dayStr(day), dayStr(day), maxRankRequest).Referrers
	if len(all) != 20 {
		t.Fatalf("不限条数时应返回全部 20 条，实际 %d 条", len(all))
	}
	if got := rankValues(all)[:limit]; !equalStrings(got, want) {
		t.Fatalf("截断查询与全量查询的前 %d 条不一致：%v", limit, got)
	}
}

// maxRankRequest 一个必然超过上限的请求值。
const maxRankRequest = 100000

// TestAnalyticsDimensionRankingAcrossWindowSwitch 窗口切换：走预聚合与走明细给出同一份榜。
//
// 这条用例钉住的是形态选择本身：维度排行**恒定读明细**，所以
// 「窗口完全落在过去」（Total/Daily/Paths 读预聚合）时它照样有数据，
// 且与含今天的窗口逐条一致 —— 不会出现「总数来自明细、来源域来自一小时前快照」的分裂。
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

// TestAnalyticsDimensionIsolationAndLimits 工程隔离、条数归一化、未知维度被白名单拒绝。
func TestAnalyticsDimensionIsolationAndLimits(t *testing.T) {
	f := newAnalyticsFixture(t)
	day := utcDay(time.Now()).AddDate(0, 0, -1)
	seedDim(t, f, dimSeed{
		path: "/a", visitor: "v1", referrer: "mine.example.com",
		uaClass: "desktop", lang: "zh-CN", at: day.Add(time.Hour),
	})
	// 另一个工程的访问（page_views 的 project_id 没有外键，任意 uuid 即可）。
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

	// 条数归一化：超过上限收敛到上限并回显，榜内容不受影响。
	capped := summaryRank(t, f, dayStr(day), dayStr(day), maxRankRequest)
	if capped.RankLimit != 200 {
		t.Errorf("rankLimit 超上限应收敛到 200，实际 %d", capped.RankLimit)
	}
	if len(capped.Referrers) != 1 {
		t.Errorf("收敛后的榜应仍返回实际存在的 1 条，实际 %d 条", len(capped.Referrers))
	}

	// 未知维度被白名单拒绝：列名在 SQL 里是标识符、绑不了参数，
	// 拼接就等于把一次调用失误升级成注入 —— 所以白名单之外的值不查库。
	m := analyticsmodel.NewModel(f.db)
	const injection = "referrer_host) FROM page_views; DROP TABLE page_views; --"
	if _, err := m.CountByDimension(context.Background(), f.projectID, day, day.AddDate(0, 0, 1), injection, 10); err == nil {
		t.Fatal("未知维度应被拒绝，而不是拼进 SQL")
	}
}
