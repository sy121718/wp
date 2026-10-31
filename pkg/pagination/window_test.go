package pagination

import (
	"strconv"
	"strings"
	"testing"
)

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
					prev = 0
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

			if len(items) > WindowSize+2 {
				t.Fatalf("pages=%d page=%d：槽位数 %d 超过上限 %d", pages, page, len(items), WindowSize+2)
			}
		}
	}
}

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

func TestWindowItemsStartEmpty(t *testing.T) {
	for _, it := range Window(5, 20) {
		if it.URL != "" {
			t.Fatalf("Window 不该拼 URL，实际 %+v", it)
		}
	}
}
