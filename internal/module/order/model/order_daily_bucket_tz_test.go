package model_test

// order_daily_bucket_tz_test.go — 桶键的时区口径（带库）。
//
// 这条测试存在的理由：日粒度与小时粒度的聚合列**形状不同**（`::date` vs
// `date_trunc(...)` → 无时区 timestamp），而 PG 驱动对两者的解释也不同：
// date 给当天零点（语义就是「某一天」），timestamp 按**本地时区**贴位置。
// 少了第二遍 `AT TIME ZONE 'UTC'` 时，在 +08 的机器上桶键会整体偏 8 小时，
// 症状是「图上多出昨天 17 点那根柱」以及「按小时查找订单时那一小时静默变 0」。
//
// 这种错误不报错、不中断，只是数字对不上，所以在 SQL 旁边写注释不够 ——
// 必须有一条会变红的判据。断言的判据是**挂钟本身**：插入 05:44Z 的单，
// 读回来的桶键 UTC 挂钟就必须是那个小时，与跑测试的机器时区无关。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
	"go_wp/public/test/support"
)

func TestDayAndHourBucketsReturnUTCWakeClock(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	project := uuid.NewString()
	support.SeedProjectRow(t, db, project, "桶时区")

	// 05:44Z 落在 05:00~06:00 这个小时；在 +08 的机器上它的本地挂钟是 13:44，
	// 正是「按本地时区贴位置」会读错的那个偏差。
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
	// 日粒度：::date 被本地时区解释、或有人把它改成 date_trunc，这里都会漂到前一天。
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
	// 小时粒度：少了第二遍 AT TIME ZONE 'UTC' 时读回来是 2026-10-04T21:00。
	if got := hours[0].Day.UTC().Format(utils.LayoutHour); got != "2026-10-05T05:00" {
		t.Errorf("按小时桶 = %q，期望 2026-10-05T05:00（少一遍 AT TIME ZONE 'UTC' 时会偏一个时区）", got)
	}
}
