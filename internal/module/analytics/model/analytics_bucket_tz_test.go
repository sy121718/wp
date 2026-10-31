package analyticsmodel_test

import (
	"context"
	"testing"
	"time"

	analyticsmodel "go_wp/internal/module/analytics/model"
	"go_wp/public/test/support"

	"github.com/google/uuid"
)

func TestViewBucketsReturnUTCWakeClock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	project := uuid.NewString()
	support.SeedProjectRow(t, db, project, "浏览桶时区")

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

	if got := hours[0].Day.UTC().Format("2006-01-02T15:00"); got != "2026-10-05T01:00" {
		t.Errorf("按小时桶 = %q，期望 2026-10-05T01:00（少一遍 AT TIME ZONE 'UTC' 时会偏一个时区）", got)
	}

	for _, h := range hours {
		if h.Day.Before(from) || !h.Day.Before(to) {
			t.Errorf("桶 %s 落在窗口 [%s, %s) 之外", h.Day.UTC().Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339))
		}
	}
}
