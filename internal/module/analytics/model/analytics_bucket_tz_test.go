package analyticsmodel_test

// analytics_bucket_tz_test.go — 浏览桶键的时区口径（带库）。
//
// 与订单侧同一个根因（见 internal/module/order/model/order_daily_bucket_tz_test.go）：
// `date_trunc(... AT TIME ZONE 'UTC')` 返回**无时区 timestamp**，PG 驱动读 time.Time
// 时按本地时区贴位置；调用方再 `.UTC()` 就整体偏一个时区。实测症状是把今天 01:00
// 的记录读成「昨天 17:00」，图上多出一根落在窗口外的柱子（窗口是 10-05，桶是 10-04），
// 并且并集后桶数从 24 变成 25。
//
// CountByDay 的 `::date` 没有这个毛病（date 的语义就是「某一天」，驱动给当天零点），
// 所以两种情况在代码里长得不一样 —— 正因如此，只写注释挡不住「有人把 ::date 统一
// 改成 date_trunc」这种顺手重构。这条测试就是那个挡板。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	analyticsmodel "go_wp/internal/module/analytics/model"
	"go_wp/public/test/support"
)

func TestViewBucketsReturnUTCWakeClock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	project := uuid.NewString()
	support.SeedProjectRow(t, db, project, "浏览桶时区")

	// 01:25Z：在 +08 的机器上本地挂钟是 09:25，正是会被读错的那个偏差。
	viewed := time.Date(2026, 10, 5, 1, 25, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO page_views (project_id, path, lang, session_id, visitor_hash, viewed_at)
		VALUES (?, '/tz-bucket', 'zh-CN', 'sess-tz', 'visitor-tz', ?)`, project, viewed).Error; err != nil {
		t.Fatal(err)
	}

	m := analyticsmodel.NewModel(db)
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	ctx := context.Background()

	days, err := m.CountByDay(ctx, project, from, to)
	if err != nil {
		t.Fatalf("CountByDay: %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("按天应只回 1 个桶，实得 %d: %+v", len(days), days)
	}
	if got := days[0].Day.UTC().Format("2006-01-02"); got != "2026-10-05" {
		t.Errorf("按天桶 = %q，期望 2026-10-05（桶键的时区口径错了）", got)
	}

	hours, err := m.CountByHour(ctx, project, from, to)
	if err != nil {
		t.Fatalf("CountByHour: %v", err)
	}
	if len(hours) != 1 {
		t.Fatalf("按小时应只回 1 个桶，实得 %d: %+v", len(hours), hours)
	}
	// 少了第二遍 AT TIME ZONE 'UTC' 时这里读回来是 2026-10-04T17:00（窗口外）。
	if got := hours[0].Day.UTC().Format("2006-01-02T15:00"); got != "2026-10-05T01:00" {
		t.Errorf("按小时桶 = %q，期望 2026-10-05T01:00（少一遍 AT TIME ZONE 'UTC' 时会偏一个时区）", got)
	}
	// 桶必须落在查询窗口内：漂出去的那根柱子在图上会被 buildTrend 当成新桶并进去。
	for _, h := range hours {
		if h.Day.Before(from) || !h.Day.Before(to) {
			t.Errorf("桶 %s 落在窗口 [%s, %s) 之外", h.Day.UTC().Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	}
}
