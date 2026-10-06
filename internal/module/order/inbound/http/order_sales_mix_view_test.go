package orderhttp

// order_sales_mix_view_test.go — 月度趋势折线的**坐标闭合**判据。
//
// 为什么单独测这个：坐标是整数除法算出来的（值 × 绘图区高 ÷ 峰值），
// 最容易出的两类缺陷都不报错、不 500，只是图上「看起来不对」——
//   ① **y 超出绘图区**：忘了减顶部留白或忘了让开底部刻度区，峰值那一点会贴到上边界、
//      甚至跑到刻度文字上；宽屏拉伸后偏差还会被放大。
//   ② **三条线不同刻度**：各自按自己的最大值归一化时，每条线单独看都「有起伏」，
//      但三条线的高低关系全错 —— 「游客单比新客高」这种事实直接看不出来。
//
// 判据因此钉两件事：**每条线的每个点都落在绘图区内**，且**三条线共用同一个峰值刻度**。
//
// 数据刻意选**除不尽**的值（777 / 111 / 222 / 444）+ 一个远超其余月份的值：
// 只测整除值时，忘了取整的写法照样能过。

import (
	"strconv"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
)

// plotTop / plotBottom 绘图区（模板 viewBox 是 1000×200，两者都在 view 层的常量里）。
const (
	testPlotTop    = salesTrendPadTop
	testPlotBottom = salesTrendViewH - salesTrendAxisH
)

func parsePoints(t *testing.T, s string) [][2]int {
	t.Helper()
	out := make([][2]int, 0)
	for _, pair := range strings.Fields(s) {
		xs, ys, ok := strings.Cut(pair, ",")
		if !ok {
			t.Fatalf("点 %q 不是 x,y 形式", pair)
		}
		x, err1 := strconv.Atoi(xs)
		y, err2 := strconv.Atoi(ys)
		if err1 != nil || err2 != nil {
			t.Fatalf("点 %q 的坐标不是整数", pair)
		}
		out = append(out, [2]int{x, y})
	}
	return out
}

func TestMonthlyTrendLineStaysInsidePlotArea(t *testing.T) {
	points := []orderdto.SalesMonthlyPointDTO{
		// 峰值月：这条线的顶点必须落在绘图区顶边上（不能贴到 0，那是裁掉描边的高度）。
		{Month: "2026-10", Sales: 1000, NewSales: 400, ReturningSales: 300, GuestSales: 300},
		// 除不尽：整数除法会让 y 差一两个像素，判据只要求「仍在区内」。
		{Month: "2026-09", Sales: 777, NewSales: 111, ReturningSales: 222, GuestSales: 444},
		// 只有一段有值（另外两条线在这个月落到基线）。
		{Month: "2026-07", Sales: 333, NewSales: 333},
		// 全 0：三条线都落到基线，且必须**同在**基线（不能一条 0 一条 1）。
		{Month: "2026-06", Sales: 0},
		// 一个月：x 落正中（没有首尾可言，贴左会像被裁掉）。
		{Month: "2026-05", Sales: 0},
	}

	chart := salesTrendView(points, "CNY", func(_, fallback string) string { return fallback })
	if !chart.HasData || !chart.HasSales {
		t.Fatal("有销售额的月份应当 HasData 且 HasSales")
	}
	if chart.Peak != 1000 {
		t.Errorf("峰值应取单月总额的最大值 1000，实得 %d（各自归一化会让三条线不可比）", chart.Peak)
	}
	if len(chart.Series) != 3 {
		t.Fatalf("应有三条线（新客 / 回头客 / 游客单），实得 %d 条", len(chart.Series))
	}

	for _, s := range chart.Series {
		pts := parsePoints(t, s.Points)
		if len(pts) != len(points) {
			t.Fatalf("%s 线的点数 %d ≠ 月份数 %d", s.Label, len(pts), len(points))
		}
		for i, p := range pts {
			if p[1] < testPlotTop || p[1] > testPlotBottom {
				t.Errorf("%s 线第 %d 个点的 y=%d 越出绘图区 [%d, %d]",
					s.Label, i, p[1], testPlotTop, testPlotBottom)
			}
			if p[0] < 0 || p[0] > salesTrendViewW {
				t.Errorf("%s 线第 %d 个点的 x=%d 越出 viewBox 宽度 %d",
					s.Label, i, p[0], salesTrendViewW)
			}
		}
		// 点的命中块与折线的点必须同坐标：分开算会让悬停读数与线错位。
		if len(s.Dots) != len(pts) {
			t.Fatalf("%s 线的命中块数 %d ≠ 点数 %d", s.Label, len(s.Dots), len(pts))
		}
		for i, d := range s.Dots {
			if d.X != pts[i][0] || d.Y != pts[i][1] {
				t.Errorf("%s 线第 %d 个命中块 (%d,%d) 与折线点 (%d,%d) 不一致",
					s.Label, i, d.X, d.Y, pts[i][0], pts[i][1])
			}
			if !strings.Contains(d.Title, points[i].Month) {
				t.Errorf("%s 线第 %d 个读数 %q 里没有月份 %s", s.Label, i, d.Title, points[i].Month)
			}
		}
	}

	// 三条线共用一个刻度：新客 400、游客 300 时，新客必须更靠上（y 更小）。
	// 各自归一化的话两条顶点都会贴到顶边，这条断言会红。
	newTop := parsePoints(t, chart.Series[0].Points)[0][1]
	guestTop := parsePoints(t, chart.Series[2].Points)[0][1]
	if newTop >= guestTop {
		t.Errorf("峰值月的新客 400 应当比游客 300 更靠上：newY=%d guestY=%d（三条线必须共用刻度）",
			newTop, guestTop)
	}
}

// 没有月份 / 一笔销售都没有：HasData 与 HasSales 必须分开表达。
//
// 回看窗口天生总有月份（补零过），所以 HasData 几乎恒真 —— 空态只能挂在 HasSales 上。
// 把两者混成一个键时，一个整月没生意的站点会得到一块空画布。
func TestMonthlyTrendEmptyFlagsAreSeparate(t *testing.T) {
	tr := func(_, fallback string) string { return fallback }

	if chart := salesTrendView(nil, "CNY", tr); chart.HasData || chart.HasSales {
		t.Error("没有月份时 HasData / HasSales 都应为 false")
	}
	zeros := []orderdto.SalesMonthlyPointDTO{{Month: "2026-10"}, {Month: "2026-09"}}
	chart := salesTrendView(zeros, "CNY", tr)
	if !chart.HasData {
		t.Error("有月份（哪怕全是 0）时 HasData 应为 true")
	}
	if chart.HasSales {
		t.Error("全是 0 时 HasSales 应为 false（空态挂在它上面）")
	}
	if chart.Peak != 0 {
		t.Errorf("全为 0 时峰值应为 0，实得 %d", chart.Peak)
	}
	// 峰值 0 时不能除零：三条线的每个点都必须落在基线上。
	for _, s := range chart.Series {
		for i, p := range parsePoints(t, s.Points) {
			if p[1] != testPlotBottom {
				t.Errorf("%s 线第 %d 个点在峰值为 0 时没有落到基线（y=%d，期望 %d）",
					s.Label, i, p[1], testPlotBottom)
			}
		}
	}
}
