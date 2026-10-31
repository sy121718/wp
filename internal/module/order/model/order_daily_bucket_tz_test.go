package model_test

import (
	"context"
	"testing"
	"time"

	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
	"go_wp/public/test/support"

	"github.com/google/uuid"
)

func TestDayAndHourBucketsReturnUTCWakeClock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	project := uuid.NewString()
	support.SeedProjectRow(t, db, project, "桶时区")

	created := time.Date(2026, 10, 5, 5, 44, 48, 0, time.UTC)
	if err := db.Exec(`INSERT INTO orders (project_id, order_no, status, attribution, create_time, update_time)
		VALUES (?, ?, 'paid', '{}'::jsonb, ?, ?)`, project, "TZ-BUCKET-1", created, created).Error; err != nil {
		t.Fatal(err)
	}

	m := ordermodel.NewOrderModel(db)
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	ctx := context.Background()

	days, err := m.DailyByRange(ctx, project, from, to)
	if err != nil {
		t.Fatalf("DailyByRange: %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("按天应只回 1 个桶，实得 %d: %+v", len(days), days)
	}

	if got := days[0].Day.UTC().Format("2006-01-02"); got != "2026-10-05" {
		t.Errorf("按天桶 = %q，期望 2026-10-05（桶键的时区口径错了）", got)
	}

	hours, err := m.HourlyByRange(ctx, project, from, to)
	if err != nil {
		t.Fatalf("HourlyByRange: %v", err)
	}
	if len(hours) != 1 {
		t.Fatalf("按小时应只回 1 个桶，实得 %d: %+v", len(hours), hours)
	}

	if got := hours[0].Day.UTC().Format(utils.LayoutHour); got != "2026-10-05T05:00" {
		t.Errorf("按小时桶 = %q，期望 2026-10-05T05:00（少一遍 AT TIME ZONE 'UTC' 时会偏一个时区）", got)
	}
}
