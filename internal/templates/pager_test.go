package templates

// pager_test.go — 分页派生值（PagerOf）的口径：夹紧、页数、本页区间、按钮态。
//
// 这些值以前由 pkg/pagination.Facts 算好放进数据；搬到模板层后判据不变 ——
// limit<1 回落 20、page<1 回落 1、页数向上取整、page 超出页数时夹到末页（取数之前收敛
// 与展示必须同口径，否则会出现「表格为空、分页条显示第 999 页」）。

import (
	"testing"

	"go_wp/pkg/pagination"
)

func TestPagerOfDerivesShape(t *testing.T) {
	cases := []struct {
		name                        string
		total                       int64
		page, limit                 int
		wantPages, wantPage         int
		wantStart, wantEnd          int
		wantNav, wantPrev, wantNext bool
	}{
		{"空集", 0, 1, 20, 0, 1, 0, 0, false, false, false},
		{"不足一页", 5, 1, 20, 1, 1, 1, 5, false, false, false},
		{"恰好一页", 20, 1, 20, 1, 1, 1, 20, false, false, false},
		{"恰好多一条", 21, 1, 20, 2, 1, 1, 20, true, false, true},
		{"整除末页", 40, 2, 20, 2, 2, 21, 40, true, true, false},
		{"末页不满", 120, 2, 20, 6, 2, 21, 40, true, true, true},
		{"页码超出上界夹到末页", 120, 99, 20, 6, 6, 101, 120, true, true, false},
		{"页码下界夹到第一页", 120, -3, 20, 6, 1, 1, 20, true, false, true},
		{"limit 非法回落 20", 120, 1, 0, 6, 1, 1, 20, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg := PagerOf(pagination.Page{Total: tc.total, Page: tc.page, Limit: tc.limit})
			if pg.Pages != tc.wantPages || pg.Page != tc.wantPage || pg.Start != tc.wantStart || pg.End != tc.wantEnd {
				t.Fatalf("派生值不对：pages=%d page=%d start=%d end=%d", pg.Pages, pg.Page, pg.Start, pg.End)
			}
			if pg.HasPagination() != tc.wantNav || pg.HasPrev() != tc.wantPrev || pg.HasNext() != tc.wantNext {
				t.Fatalf("按钮态不对：nav=%v prev=%v next=%v", pg.HasPagination(), pg.HasPrev(), pg.HasNext())
			}
		})
	}
}

// TestPagerDefaultsKeepLimitInLinks 链接默认带 limit（与迁移前所有站点的链接一致）；
// DropLimit 是唯一的例外开关（mail 账号页、商品标签命中片段）。
func TestPagerDefaultsKeepLimitInLinks(t *testing.T) {
	keep := PagerOf(pagination.Page{Total: 120, Page: 2, Limit: 20})
	if keep.PageParam != "page" || !keep.KeepLimit {
		t.Fatalf("默认应为 page / 带 limit，实际 %q/%v", keep.PageParam, keep.KeepLimit)
	}
	drop := PagerOf(pagination.Page{Total: 120, Page: 2, Limit: 20, PageParam: "account_page", DropLimit: true})
	if drop.PageParam != "account_page" || drop.KeepLimit {
		t.Fatalf("显式给的值没生效：%q/%v", drop.PageParam, drop.KeepLimit)
	}
}

func TestPagerZeroValueIsInert(t *testing.T) {
	var zero Pager
	if zero.HasPagination() || zero.HasPrev() || zero.HasNext() {
		t.Fatalf("零值不该有可渲染行为：%+v", zero)
	}
}
