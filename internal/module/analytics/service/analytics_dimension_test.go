package analyticsservice

import (
	"testing"

	analyticsmodel "go_wp/internal/module/analytics/model"
)

func TestNormalizeRankLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, defaultRankLimit},
		{-5, defaultRankLimit},
		{1, 1},
		{defaultRankLimit, defaultRankLimit},
		{maxRankLimit, maxRankLimit},
		{maxRankLimit + 1, maxRankLimit},
		{100000, maxRankLimit},
	}
	for _, c := range cases {
		if got := normalizeRankLimit(c.in); got != c.want {
			t.Errorf("normalizeRankLimit(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

func TestToRankCountsKeepsShape(t *testing.T) {
	empty := toRankCounts(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("空结果应是长度 0 的非 nil 切片，实际 %#v", empty)
	}
	rows := []analyticsmodel.DimensionRow{
		{Value: "", Views: 3, Visitors: 2},
		{Value: "ref.example.com", Views: 1, Visitors: 1},
	}
	out := toRankCounts(rows)
	if len(out) != 2 {
		t.Fatalf("应转换出 2 条，实际 %d 条", len(out))
	}
	if out[0].Value != "" || out[0].Views != 3 || out[0].Visitors != 2 {
		t.Fatalf("空取值应原样保留（空串是合法取值）：%+v", out[0])
	}
	if out[1].Value != "ref.example.com" || out[1].Views != 1 || out[1].Visitors != 1 {
		t.Fatalf("取值与计数应原样保留：%+v", out[1])
	}
}
