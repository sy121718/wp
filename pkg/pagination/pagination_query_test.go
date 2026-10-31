package pagination

import (
	"net/url"
	"strings"
	"testing"
)

func TestBaseURL(t *testing.T) {
	cases := []struct {
		name string
		path string
		q    url.Values
		want string
	}{
		{"无条件", "/admin/product-tags", url.Values{}, "/admin/product-tags"},
		{"nil 查询串", "/admin/product-tags", nil, "/admin/product-tags"},
		{"只带工程", "/admin/product-brands", Query("project", "p1", "keyword", ""), "/admin/product-brands?project=p1"},
		{"工程 + 关键词", "/admin/product-categories", Query("project", "p1", "keyword", "户外"),
			"/admin/product-categories?keyword=%E6%88%B7%E5%A4%96&project=p1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BaseURL(c.path, c.q)
			if got != c.want {
				t.Fatalf("基地址 = %q，期望 %q", got, c.want)
			}

			if strings.Contains(got, "page=") || strings.Contains(got, "limit=") {
				t.Fatalf("基地址不应含 page/limit：%q", got)
			}
		})
	}
}

func TestQuerySkipsBlanksAndOddTail(t *testing.T) {
	q := Query("project", "  ", "keyword", " 户外 ", "status", "")
	if q.Get("project") != "" {
		t.Errorf("空白值应被跳过，得到 project=%q", q.Get("project"))
	}
	if q.Get("keyword") != "户外" {
		t.Errorf("值应 trim，得到 %q", q.Get("keyword"))
	}
	if _, ok := q["status"]; ok {
		t.Errorf("空值不应落进查询串：%v", q)
	}
	if got := Query("a", "1", "b"); got.Get("b") != "" || len(got) != 1 {
		t.Errorf("落单的键应被忽略，得到 %v", got)
	}
}

func TestClampPage(t *testing.T) {
	cases := []struct {
		name  string
		page  int
		size  int
		total int64
		want  int
	}{
		{"页内", 2, 20, 45, 2},
		{"越界收敛到末页", 99, 20, 45, 3},
		{"越界的 999 收敛到末页", 999, 20, 45, 3},
		{"零页码退回第一页", 0, 20, 45, 1},
		{"负页码退回第一页", -7, 20, 45, 1},
		{"空数据（total=0）", 5, 20, 0, 1},
		{"size 异常用默认每页条数", 1, 0, int64(DefaultPageSize), 1},
		{"刚好整页", 2, 20, 40, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClampPage(c.page, c.size, c.total); got != c.want {
				t.Fatalf("收敛后页码 = %d，期望 %d", got, c.want)
			}
		})
	}
}

func TestSlice(t *testing.T) {
	all := []int{1, 2, 3, 4, 5}
	rows, cur := Slice(all, 2, 2)
	if cur != 2 || len(rows) != 2 || rows[0] != 3 {
		t.Fatalf("第 2 页 = %v（cur=%d），期望 [3 4] 与 2", rows, cur)
	}
	rows, cur = Slice(all, 999, 2)
	if cur != 3 || len(rows) != 1 || rows[0] != 5 {
		t.Fatalf("越界收敛 = %v（cur=%d），期望 [5] 与 3", rows, cur)
	}
	rows, cur = Slice([]int{}, 3, 20)
	if cur != 1 || len(rows) != 0 {
		t.Fatalf("空列表 = %v（cur=%d），期望 [] 与 1", rows, cur)
	}
}
