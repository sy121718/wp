package producthttp

// product_list_paging_test.go — 商品域后台子列表（分类 / 品牌 / 标签）的分页与取址纯逻辑。
//
// 这一组不碰数据库、不渲染模板，只钉住两件容易静默出错的事：
//   - 页码收敛：page=999 / page=0 / 空列表时切出来的区间必须是「最后一页 / 第一页 / 空」，
//     不能是「表格为空但分页条显示第 999 页」这种自相矛盾的组合（BuildPagination 拿到的
//     total 与 page 不同源时就会这样）；
//   - 分页基地址：筛选条件必须进基地址、page/limit 必须不进 —— 基地址里塞了 page 会出现
//     两个 page 参数（浏览器取第一个），表现是「点了下一页没反应」，而且不会报错。
//
// 属性页已不在 listPageSlice 上（它的请求类型自带 Page/Size，分页整条链下推到 service，
// 总数来自契约的 CountAttributes），故这里同时钉住它用的 clampPageToTotal：
// **取数之前**的收敛才能避免「空表格 + 分页条显示第 N 页」。
//
// 位置：放模块内就近单测（internal/module/**），与项目「纯逻辑就近跑」的分层一致。

import (
	"net/url"
	"strings"
	"testing"
)

// TestListPageSlice 页码 → 区间 的收敛表（含三种越界与空列表）。
func TestListPageSlice(t *testing.T) {
	all := make([]int, 0, 45)
	for i := 1; i <= 45; i++ {
		all = append(all, i)
	}

	cases := []struct {
		name     string
		all      []int
		page     int
		size     int
		wantLen  int
		wantCur  int
		wantHead int
		wantTail int
	}{
		{"第一页", all, 1, 20, 20, 1, 1, 20},
		{"第二页", all, 2, 20, 20, 2, 21, 40},
		{"最后一页不满", all, 3, 20, 5, 3, 41, 45},
		// 越界页收敛到最后一页：不收敛的话区间是空的，而分页条仍按 page=99 渲染。
		{"页码越界收敛到末页", all, 99, 20, 5, 3, 41, 45},
		{"页码为 0 退回第一页", all, 0, 20, 20, 1, 1, 20},
		{"页码为负退回第一页", all, -7, 20, 20, 1, 1, 20},
		// size 异常时用列表页的默认每页条数（不允许出现「一次返回全量」的退化）。
		{"size 为 0 用默认值", all, 1, 0, productSubListPageSize, 1, 1, productSubListPageSize},
		{"刚好整页", all, 1, 45, 45, 1, 1, 45},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, cur := listPageSlice(c.all, c.page, c.size)
			if len(rows) != c.wantLen {
				t.Fatalf("本页条数 = %d，期望 %d", len(rows), c.wantLen)
			}
			if cur != c.wantCur {
				t.Fatalf("收敛后页码 = %d，期望 %d", cur, c.wantCur)
			}
			if c.wantLen > 0 {
				if rows[0] != c.wantHead || rows[len(rows)-1] != c.wantTail {
					t.Fatalf("区间 = [%d..%d]，期望 [%d..%d]", rows[0], rows[len(rows)-1], c.wantHead, c.wantTail)
				}
			}
		})
	}
}

// TestListPageSliceEmptyList 空列表：任何页码都返回空行 + 第 1 页。
//
// 空数据时 BuildPagination 拿到 total=0 会返回 nil（模板不渲染分页条），
// 但这里仍需给出可用的 current —— 否则调用方会把 0 传给分页组件。
func TestListPageSliceEmptyList(t *testing.T) {
	rows, cur := listPageSlice([]string{}, 3, productSubListPageSize)
	if len(rows) != 0 {
		t.Fatalf("空列表应切出 0 行，得到 %d", len(rows))
	}
	if cur != 1 {
		t.Fatalf("空列表的页码应收敛为 1，得到 %d", cur)
	}
}

// TestClampPageToTotal 取数之前的页码收敛表（属性页 / 库存三页共用这条规则）。
//
// 与 listPageSlice 的收敛判据一致，区别只在**发生时机**：它必须在调用 service 之前跑，
// 否则越界页码会让列表接口返回空页，而分页条已经按收敛后的页码渲染了。
func TestClampPageToTotal(t *testing.T) {
	cases := []struct {
		name  string
		page  int
		size  int
		total int64
		want  int
	}{
		{"首页", 1, 20, 45, 1},
		{"中间页", 2, 20, 45, 2},
		{"末页", 3, 20, 45, 3},
		{"越界收敛到末页", 99, 20, 45, 3},
		{"越界的 999 收敛到末页", 999, 20, 45, 3},
		{"零页码退回第一页", 0, 20, 45, 1},
		{"负页码退回第一页", -7, 20, 45, 1},
		{"空数据（total=0）", 5, 20, 0, 1},
		{"size 异常用默认每页条数", 1, 0, int64(productSubListPageSize), 1},
		{"刚好整页", 2, 20, 40, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clampPageToTotal(c.page, c.size, c.total); got != c.want {
				t.Fatalf("收敛后页码 = %d，期望 %d", got, c.want)
			}
		})
	}
}

// TestProductListBaseURL 分页基地址：筛选条件进、page/limit 不进。
func TestProductListBaseURL(t *testing.T) {
	cases := []struct {
		name string
		path string
		q    url.Values
		want string
	}{
		{"无条件", "/admin/product-tags", url.Values{}, "/admin/product-tags"},
		{"只带工程", "/admin/product-brands", listFilterQuery("p1", ""), "/admin/product-brands?project=p1"},
		{"工程 + 关键词", "/admin/product-categories", listFilterQuery("p1", "户外"),
			"/admin/product-categories?keyword=%E6%88%B7%E5%A4%96&project=p1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := productListBaseURL(c.path, c.q)
			if got != c.want {
				t.Fatalf("基地址 = %q，期望 %q", got, c.want)
			}
			// page / limit 由 shell.BuildPagination 自己追加：基地址里出现它们就会有两个同名参数。
			if strings.Contains(got, "page=") || strings.Contains(got, "limit=") {
				t.Fatalf("基地址不应含 page/limit：%q", got)
			}
		})
	}
}

// TestAttributeListFilterQuery 属性页多带一维 variation，且空值不写进查询串。
func TestAttributeListFilterQuery(t *testing.T) {
	q := attributeListFilterQuery("p1", "颜色", "1")
	if q.Get("variation") != "1" || q.Get("keyword") != "颜色" || q.Get("project") != "p1" {
		t.Fatalf("筛选条件不全：%v", q)
	}
	if got := attributeListFilterQuery("p1", "颜色", ""); got.Get("variation") != "" || got.Has("variation") {
		t.Fatalf("variation 为空时不应写进查询串：%v", got)
	}
}
