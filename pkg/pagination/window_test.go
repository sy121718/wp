package pagination

// window_test.go — 窗口口径的判据。
//
// 这一层的测试必须是**表驱动 + 穷举扫描**，不能只测几个手工挑的例子：
// Laravel issue #32943 与 Pagy issue #152 都是「中段正常、贴边组合出坏形状」，
// 而贴边的坏形状（重复页、缺页、页码跑出 1..pages）恰恰是抽查最容易漏掉的部分。

import (
	"strconv"
	"strings"
	"testing"
)

// legacyWindow 把迁移前 internal/templates/pageNumbers 的算法**逐字拷进来**作 oracle。
//
// 为什么不直接调用旧实现：那个函数随模板层改造会被删除，把它复制成 oracle 才能逐页比较
// 「窗口是否真的没变」—— 这是唯一能自证「策略未漂移」的办法。
func legacyWindow(page, pages, win int) []int {
	if pages <= 1 {
		return nil
	}
	if win < 3 {
		win = 3
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	if pages <= win {
		out := make([]int, 0, pages)
		for p := 1; p <= pages; p++ {
			out = append(out, p)
		}
		return out
	}
	half := (win - 3) / 2
	lo := page - half
	hi := page + half
	if lo < 2 {
		lo, hi = 2, lo+win-3
	}
	if hi > pages-1 {
		hi, lo = pages-1, pages-1-(win-3)
	}
	out := make([]int, 0, win)
	out = append(out, 1)
	for p := lo; p <= hi; p++ {
		if p < 2 || p > pages-1 {
			continue
		}
		out = append(out, p)
	}
	out = append(out, pages)
	return out
}

// labels 把槽位还原成标签序列（省略号 → "…"）。
func labels(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Gap {
			out = append(out, "…")
			continue
		}
		out = append(out, strconv.Itoa(it.Page))
	}
	return out
}

// legacyLabels 把旧算法输出的整数序列还原成**同一形态**的标签序列。
//
// 为什么要过这一步：旧实现只产出页码，省略号是模板按「相邻差 > 1」画出来的
// （旧 partial 里那段 {{if n-prev > 1}}）。所以拿整数序列直接对比标签序列是错的 ——
// 比的是两种不同的东西。这里把旧约定的画法补上，两边才是同一口径。
func legacyLabels(nums []int) []string {
	out := make([]string, 0, len(nums))
	prev := 0
	for _, n := range nums {
		if prev > 0 && n-prev > 1 {
			out = append(out, "…")
		}
		out = append(out, strconv.Itoa(n))
		prev = n
	}
	return out
}

// TestWindowMatchesLegacyEveryCombination 扫 pages=0..40 × page=-2..pages+2，逐页对照旧算法。
//
// 含越界页码与 0 页：旧实现靠「先夹紧」处理这些，新代码必须同口径。
func TestWindowMatchesLegacyEveryCombination(t *testing.T) {
	for pages := 0; pages <= 40; pages++ {
		for page := -2; page <= pages+2; page++ {
			got := labels(Window(page, pages))
			want := legacyLabels(legacyWindow(page, pages, WindowSize))
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("pages=%d page=%d：窗口 %v，旧算法 %v", pages, page, got, want)
			}
		}
	}
}

// TestWindowShapeInvariants 形态不变量：不管 pages/page 取什么值，
// 槽位数、页码单调递增、省略号不在两端、首尾必是 1 与 pages。
//
// 这几条正是 Laravel #32943（重复页）与 Pagy #152（首尾多出页码）破掉的性质。
func TestWindowShapeInvariants(t *testing.T) {
	for pages := 2; pages <= 200; pages++ {
		for page := 1; page <= pages; page++ {
			items := Window(page, pages)
			if len(items) == 0 {
				t.Fatalf("pages=%d page=%d：多于一页却给出空窗口", pages, page)
			}
			if items[0].Gap || items[0].Page != 1 {
				t.Fatalf("pages=%d page=%d：首槽位必须是第 1 页，实际 %+v", pages, page, items[0])
			}
			last := items[len(items)-1]
			if last.Gap || last.Page != pages {
				t.Fatalf("pages=%d page=%d：尾槽位必须是第 %d 页，实际 %+v", pages, page, pages, last)
			}
			prev := 0
			for i, it := range items {
				if it.Gap {
					if i == 0 || i == len(items)-1 {
						t.Fatalf("pages=%d page=%d：省略号不能在两端，实际 %v", pages, page, labels(items))
					}
					prev = 0 // 省略号后重新开始计差
					continue
				}
				if it.Page < 1 || it.Page > pages {
					t.Fatalf("pages=%d page=%d：页码 %d 跑出了 1..%d", pages, page, it.Page, pages)
				}
				if prev != 0 && it.Page <= prev {
					t.Fatalf("pages=%d page=%d：页码未严格递增（%d 在 %d 之后），实际 %v",
						pages, page, it.Page, prev, labels(items))
				}
				prev = it.Page
			}
			// 槽位数上限：1 + 1 + 窗口 + 1 + 1 是理论最大（两处省略号）
			if len(items) > WindowSize+2 {
				t.Fatalf("pages=%d page=%d：槽位数 %d 超过上限 %d", pages, page, len(items), WindowSize+2)
			}
		}
	}
}

// TestWindowClampsAndDegenerate pages<=1 与越界页码的退化形态必须是确定性的。
func TestWindowClampsAndDegenerate(t *testing.T) {
	for _, pages := range []int{0, 1, -3} {
		if got := Window(1, pages); got != nil {
			t.Fatalf("pages=%d 应回空窗口，实际 %v", pages, labels(got))
		}
	}
	if got, want := labels(Window(99, 6)), legacyLabels(legacyWindow(6, 6, WindowSize)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("页码越界应先夹到末页：%v", got)
	}
	if got, want := labels(Window(0, 5)), legacyLabels(legacyWindow(1, 5, WindowSize)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("页码下界应先夹到首页：%v", got)
	}
}

// TestWindowNoDuplicatePages 显式钉住 Laravel #32943 报的那个缺陷：
// onEachSide 参数化之后贴边时数字重复出现。本实现不参数化，所以任何 pages/page 组合都不许重复。
func TestWindowNoDuplicatePages(t *testing.T) {
	for pages := 2; pages <= 120; pages++ {
		for page := 1; page <= pages; page++ {
			seen := map[int]bool{}
			for _, it := range Window(page, pages) {
				if it.Gap {
					continue
				}
				if seen[it.Page] {
					t.Fatalf("pages=%d page=%d：第 %d 页重复出现（Laravel issue #32943 的形态）",
						pages, page, it.Page)
				}
				seen[it.Page] = true
			}
		}
	}
}

// TestWindowItemsStartEmpty 窗口槽位的 URL 由 Pager.Items 填，Window 自己必须留空：
// 两层职责分开，Window 才能在不知道基地址的情况下被单测覆盖。
func TestWindowItemsStartEmpty(t *testing.T) {
	for _, it := range Window(5, 20) {
		if it.URL != "" {
			t.Fatalf("Window 不该拼 URL，实际 %+v", it)
		}
	}
}
