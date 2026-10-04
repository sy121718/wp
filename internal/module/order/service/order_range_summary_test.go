package orderservice

// order_range_summary_test.go — 区间窗口归一化（纯逻辑，不碰库）。
//
// 窗口规则是「概览页 KPI 到底算了哪一段」的全部依据，而它只靠日期串算，
// 不需要数据库 —— 所以钉在这里，不放进 feature 链路。

import (
	"testing"
	"time"

	orderenums "go_wp/internal/module/order/enums"
)

// TestNormalizeRangeWindow 窗口归一化：含当天、半开上界、未来收敛、非法拒绝。
//
// now 固定为 2026-10-04 15:30 UTC（否则「未来日期收敛到今天」这条只能等一天才能验）。
func TestNormalizeRangeWindow(t *testing.T) {
	now := time.Date(2026, 10, 4, 15, 30, 0, 0, time.UTC)
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	tests := []struct {
		name     string
		rawFrom  string
		rawTo    string
		wantFrom time.Time
		wantTo   time.Time
		wantErr  bool
	}{
		{
			name: "两端都合法：上界是次日的零点（半开）",
			// 「含 10-04」= 上界取 10-05 的零点，这样 10-04 全天都在窗口内。
			rawFrom: "2026-10-01", rawTo: "2026-10-04",
			wantFrom: day(2026, 10, 1), wantTo: day(2026, 10, 5),
		},
		{
			name:    "单日区间：from 等于 to 仍然有效",
			rawFrom: "2026-10-04", rawTo: "2026-10-04",
			wantFrom: day(2026, 10, 4), wantTo: day(2026, 10, 5),
		},
		{
			name:    "上界晚于明天：收敛到明天（未来的日期没有数据）",
			rawFrom: "2026-10-01", rawTo: "2026-12-31",
			wantFrom: day(2026, 10, 1), wantTo: day(2026, 10, 5),
		},
		{
			name:    "当天：from 与 to 都是今天",
			rawFrom: "2026-10-04", rawTo: "2026-10-04",
			wantFrom: day(2026, 10, 4), wantTo: day(2026, 10, 5),
		},
		{
			name: "上界就是今天：不收敛也不报错",
			// to 的次日 = 今天+1，与收敛上界相等 —— 边界两侧行为要一致。
			rawFrom: "2026-10-01", rawTo: "2026-10-04",
			wantFrom: day(2026, 10, 1), wantTo: day(2026, 10, 5),
		},
		{name: "起始为空：拒绝（不猜默认窗口）", rawFrom: "", rawTo: "2026-10-04", wantErr: true},
		{name: "结束为空：拒绝", rawFrom: "2026-10-01", rawTo: "", wantErr: true},
		{name: "起始非法：拒绝", rawFrom: "2026/10/01", rawTo: "2026-10-04", wantErr: true},
		{name: "结束非法：拒绝", rawFrom: "2026-10-01", rawTo: "20261004", wantErr: true},
		{name: "起始晚于结束：拒绝", rawFrom: "2026-10-05", rawTo: "2026-10-04", wantErr: true},
		{
			name:    "跨度超过上限：拒绝",
			rawFrom: "2025-01-01", rawTo: "2026-10-04",
			wantErr: true,
		},
		{
			name:    "跨度刚好等于上限：接受",
			// 366 天 = maxRangeDays，边界应放行（否则「一年」这个常用区间会被拒）。
			rawFrom: "2025-10-04", rawTo: "2026-10-04",
			wantFrom: day(2025, 10, 4), wantTo: day(2026, 10, 5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := normalizeRangeWindow(tt.rawFrom, tt.rawTo, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("应当报错，实得窗口 [%v, %v)", from, to)
				}
				if err.Error() != orderenums.ErrInvalidParam {
					t.Fatalf("错误文案应为 enums 的可翻译 key，实得 %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if !from.Equal(tt.wantFrom) {
				t.Errorf("from 应为 %v，实得 %v", tt.wantFrom, from)
			}
			if !to.Equal(tt.wantTo) {
				t.Errorf("to 应为 %v，实得 %v", tt.wantTo, to)
			}
		})
	}
}
