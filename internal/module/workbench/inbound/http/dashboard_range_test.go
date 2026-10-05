package workbenchhttp

// dashboard_range_test.go — 概览页时间区间的判据（纯函数，不连库、不起路由）。
//
// 这里钉的是三件事：
//  1. 预设键 → from/to 的映射（含 ISO 周以周一为起点）；
//  2. 自定义区间的四种收敛，以及**收敛要被记下来**（Clamped）——静默改口径比报错更糟；
//  3. 趋势按点数排版：柱子不越出画布、标签稀疏到可读。
//
// 用固定的 today 而不是 time.Now()：区间逻辑全是「相对今天」的算术，
// 用真实时钟会让用例在周一 / 月末 / 年末有不同的行为（也就会周期性变红）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// rangeCtx 造一个只带 query 的 gin 上下文。
func rangeCtx(target string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}

// rangeTodayUTC 用例的「今天」：2026-10-05（周一）。
func rangeTodayUTC() time.Time {
	return time.Date(2026, 10, 5, 8, 30, 0, 0, time.UTC)
}

func TestParseOverviewRangePresets(t *testing.T) {
	today := rangeTodayUTC()
	cases := []struct {
		query     string
		wantKey   string
		wantFrom  string
		wantTo    string
		wantDays  int
		wantGran  string
	}{
		// 1~2 天的区间默认按小时：按天看一天只有一根柱子，形状和时段分布都读不出来。
		{"?range=today", rangeToday, "2026-10-05", "2026-10-05", 1, trendGranularityHour},
		{"?range=yesterday", rangeYesterday, "2026-10-04", "2026-10-04", 1, trendGranularityHour},
		{"?range=week", rangeWeek, "2026-10-05", "2026-10-05", 1, trendGranularityHour},
		{"?range=month", rangeMonth, "2026-10-01", "2026-10-05", 5, trendGranularityDay},
		// 本年 278 天 > 31 → 按周聚合（278 根柱子画不出来）。
		{"?range=year", rangeYear, "2026-01-01", "2026-10-05", 278, trendGranularityWeek},
		// 认不出的键（老链接、手改的 URL）按默认走：不报错，也不猜语义。
		{"?range=bogus", rangeWeek, "2026-10-05", "2026-10-05", 1, trendGranularityHour},
		{"", rangeWeek, "2026-10-05", "2026-10-05", 1, trendGranularityHour},
		// 显式覆盖：本月虽然默认按天，但可以要求按周。
		{"?range=month&granularity=week", rangeMonth, "2026-10-01", "2026-10-05", 5, trendGranularityWeek},
		// 白名单外忽略（保持默认），不报错。
		{"?range=month&granularity=bogus", rangeMonth, "2026-10-01", "2026-10-05", 5, trendGranularityDay},
		// 按小时超出区间上限（> 2 天）→ 回落默认粒度，而不是把请求发下去让 service 报错。
		{"?range=month&granularity=hour", rangeMonth, "2026-10-01", "2026-10-05", 5, trendGranularityDay},
	}
	for _, tc := range cases {
		got := parseOverviewRange(rangeCtx("/admin/dashboard"+tc.query), today)
		if got.Key != tc.wantKey || got.From != tc.wantFrom || got.To != tc.wantTo {
			t.Errorf("%q：区间 = %s [%s~%s]，期望 %s [%s~%s]",
				tc.query, got.Key, got.From, got.To, tc.wantKey, tc.wantFrom, tc.wantTo)
		}
		if got.Days != tc.wantDays {
			t.Errorf("%q：天数 = %d，期望 %d", tc.query, got.Days, tc.wantDays)
		}
		if got.Granularity != tc.wantGran {
			t.Errorf("%q：粒度 = %q，期望 %q", tc.query, got.Granularity, tc.wantGran)
		}
		if got.Clamped {
			t.Errorf("%q：预设区间不该标为收敛", tc.query)
		}
	}
}

// TestParseOverviewRangeWeekStartsMonday 钉 ISO 周：周一为起点。
//
// 先断言锚点本身（2026-10-05 是周一），锚点假设错了要报「前提不成立」，
// 而不是让用例替实现背锅。
func TestParseOverviewRangeWeekStartsMonday(t *testing.T) {
	today := rangeTodayUTC()
	if today.Weekday() != time.Monday {
		t.Fatalf("用例前提不成立：2026-10-05 应是周一，实为 %s", today.Weekday())
	}
	// 同周的周三，本周区间必须与周一完全相同（否则「本周」会随看的日子漂移）。
	wednesday := today.AddDate(0, 0, 2)
	if wednesday.Weekday() != time.Wednesday {
		t.Fatalf("用例前提不成立：应是周三，实为 %s", wednesday.Weekday())
	}
	mon := parseOverviewRange(rangeCtx("/admin/dashboard?range=week"), today)
	wed := parseOverviewRange(rangeCtx("/admin/dashboard?range=week"), wednesday)
	if mon.From != wed.From {
		t.Errorf("同一 ISO 周的起点应相同：周一算出 %s、周三算出 %s", mon.From, wed.From)
	}
	if mon.From != "2026-10-05" {
		t.Errorf("本周起点应是周一 2026-10-05，实得 %s", mon.From)
	}
	if wed.To != "2026-10-07" {
		t.Errorf("周三看到的本周终点应是当天，实得 %s", wed.To)
	}
}

func TestParseCustomRangeClamps(t *testing.T) {
	today := rangeTodayUTC()
	cases := []struct {
		name      string
		query     string
		wantFrom  string
		wantTo    string
		wantClamp bool
	}{
		{"正常区间", "?range=custom&from=2026-09-01&to=2026-09-10", "2026-09-01", "2026-09-10", false},
		// 顺序写反：用户想表达的是「这几天」，不该变成空区间。
		{"首尾颠倒", "?range=custom&from=2026-09-10&to=2026-09-01", "2026-09-01", "2026-09-10", true},
		// 未来日期：未来的订单不存在，窗口伸到未来只会把「日均」拉小。
		{"终点在未来", "?range=custom&from=2026-10-01&to=2026-12-31", "2026-10-01", "2026-10-05", true},
		// 超长：保留 to 往前截，保住「最近」这一半。
		{"超长区间", "?range=custom&from=2020-01-01&to=2026-10-05", "2025-10-05", "2026-10-05", true},
		// 解析不出来（复制截断、手改）→ 回落默认区间。
		{"日期非法", "?range=custom&from=oops&to=2026-10-05", "2026-10-05", "2026-10-05", false},
	}
	for _, tc := range cases {
		got := parseOverviewRange(rangeCtx("/admin/dashboard"+tc.query), today)
		if got.From != tc.wantFrom || got.To != tc.wantTo {
			t.Errorf("%s：区间 = [%s~%s]，期望 [%s~%s]", tc.name, got.From, got.To, tc.wantFrom, tc.wantTo)
		}
		if got.Clamped != tc.wantClamp {
			t.Errorf("%s：Clamped = %v，期望 %v（静默改口径比报错更糟）", tc.name, got.Clamped, tc.wantClamp)
		}
	}
}

// TestCustomRangeMaxDays 钉收敛后的上限：最长 366 天含首尾。
func TestCustomRangeMaxDays(t *testing.T) {
	today := rangeTodayUTC()
	got := parseOverviewRange(rangeCtx("/admin/dashboard?range=custom&from=2020-01-01&to=2026-10-05"), today)
	if got.Days != rangeMaxDays {
		t.Errorf("收敛后应为 %d 天，实得 %d", rangeMaxDays, got.Days)
	}
}

func TestNewRangeGranularity(t *testing.T) {
	cases := []struct {
		days     int
		wantGran string
	}{
		// 1~2 天按小时（一天的按天图只有一根柱子）。
		{1, trendGranularityHour}, {2, trendGranularityHour},
		// 3~31 天按天。
		{3, trendGranularityDay}, {31, trendGranularityDay},
		// 超过 31 天按周。
		{32, trendGranularityWeek}, {366, trendGranularityWeek},
	}
	for _, tc := range cases {
		from := "2026-01-01"
		to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, tc.days-1).Format("2006-01-02")
		got := newRange(rangeCustom, from, to, false)
		if got.Granularity != tc.wantGran {
			t.Errorf("%d 天：粒度 = %q，期望 %q", tc.days, got.Granularity, tc.wantGran)
		}
	}
}

func TestRangePresetURLKeepsOtherFilters(t *testing.T) {
	c := rangeCtx("/admin/dashboard?project=p1&range=custom&from=2026-09-01&to=2026-09-10")
	// 切回预设：from/to 必须被丢掉，否则新区间会带着旧的自定义日期一起提交。
	preset := rangePresetURL(c, rangeWeek, "2026-09-01", "2026-09-10")
	if preset != "/admin?project=p1&range=week" {
		t.Errorf("预设链接 = %q（应保留 project、丢掉 from/to，且指向 /admin）", preset)
	}
	// 自定义：from/to 要带上（供表单回显当前区间）。
	custom := rangePresetURL(c, rangeCustom, "2026-09-01", "2026-09-10")
	for _, want := range []string{"project=p1", "range=custom", "from=2026-09-01", "to=2026-09-10"} {
		if !strings.Contains(custom, want) {
			t.Errorf("自定义链接 %q 应含 %q", custom, want)
		}
	}
}

// TestRangePresetsMarksActive 钉选中态：一排按钮里恰好一个 active。
func TestRangePresetsMarksActive(t *testing.T) {
	c := rangeCtx("/admin/dashboard?range=month")
	got := rangePresets(c, parseOverviewRange(c, rangeTodayUTC()))
	if len(got) != 6 {
		t.Fatalf("预设按钮应有 6 个，实得 %d", len(got))
	}
	active := 0
	for _, p := range got {
		if p.Active {
			active++
		}
		// 词条键必须由服务端给全：模板不做字符串拼接。
		if p.LabelKey != "admin.dashboard.range."+p.Key {
			t.Errorf("按钮 %s 的词条键 = %q", p.Key, p.LabelKey)
		}
	}
	if active != 1 {
		t.Errorf("选中态应恰好 1 个，实得 %d", active)
	}
}

// TestLayoutTrendBarsFitsChart 钉排版：柱子不越出画布，标签稀疏到可读。
func TestLayoutTrendBarsFitsChart(t *testing.T) {
	for _, n := range []int{1, 7, 30, 53} {
		points := make([]overviewTrendPoint, n)
		for i := range points {
			points[i].Day = "2026-01-01"
		}
		layoutTrendBars(points)

		last := points[n-1]
		if last.X+last.BarWidth > trendChartWidth {
			t.Errorf("%d 根柱子：最后一根右边到 %d，超出画布 %d（SVG 会直接裁掉，页面不报错）",
				n, last.X+last.BarWidth, trendChartWidth)
		}
		labels := 0
		for _, p := range points {
			if p.BarWidth < 2 {
				t.Errorf("%d 根柱子：柱宽 %d < 2（宽度为 0 的矩形不渲染，表现为「图里少了几天」）", n, p.BarWidth)
			}
			if p.ShowLabel {
				labels++
			}
		}
		if labels > trendLabelMax+1 {
			t.Errorf("%d 根柱子：标签 %d 个，超过稀疏上限 %d", n, labels, trendLabelMax)
		}
		if !points[0].ShowLabel || !points[n-1].ShowLabel {
			t.Errorf("%d 根柱子：首尾必须带标签（区间两端是读者最想确认的）", n)
		}
	}
}

// TestBuildTrendWeeklyMergesViews 周粒度对**浏览量那一侧**同样生效。
//
// 按天画一年的柱子看不清，所以长区间要按周聚合 —— 这条对两张图都成立。
// 只合并销售额而把浏览量留在按天粒度，会让两张图的横坐标对不上（同一个 Tab 组里
// 两张图共用一根日期轴）。
func TestBuildTrendWeeklyMergesViews(t *testing.T) {
	// 先钉用例前提：2026-09-28 是周一（ISO 周起点）。
	if time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC).Weekday() != time.Monday {
		t.Fatalf("用例前提不成立：2026-09-28 应是周一")
	}
	byDay := map[string]*overviewTrendPoint{
		"2026-09-29": {Orders: 1, NetSales: 100},
		"2026-09-30": {Orders: 2, NetSales: 200},
	}
	views := map[string]int64{"2026-09-29": 10, "2026-10-01": 5}

	got := buildTrend(byDay, views, trendGranularityWeek, "¥")
	if len(got) != 1 {
		t.Fatalf("同一 ISO 周的三天应合并成 1 根柱子，实得 %d：%+v", len(got), got)
	}
	p := got[0]
	if p.Day != "2026-09-28" {
		t.Errorf("合并后的日期应取那一周的周一（2026-09-28），实得 %s", p.Day)
	}
	if p.Orders != 3 || p.NetSales != 300 {
		t.Errorf("订单侧应合并为 3 单 / 300 分，实得 %d / %d", p.Orders, p.NetSales)
	}
	// 10-01 那天没有订单、只有浏览：它也要被并进这根柱子（否则浏览量图会少一截）。
	if p.Views != 15 {
		t.Errorf("浏览量应合并为 15（10 + 5，含只有浏览没有订单的那天），实得 %d", p.Views)
	}

	// 按天时三天各一根。
	daily := buildTrend(byDay, views, trendGranularityDay, "¥")
	if len(daily) != 3 {
		t.Errorf("按天应得到 3 根柱子（订单两天 + 只有浏览的一天），实得 %d", len(daily))
	}
}
