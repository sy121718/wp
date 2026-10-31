package mailhttp

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
)

func TestMailListPaginationFacts(t *testing.T) {
	pg := mailListPaginationFacts(45, 1, "account_page", "/admin/mail")
	if pg.PageParam != "account_page" {
		t.Fatalf("页码参数名应归域内 account_page，实际 %q", pg.PageParam)
	}
	if pg.Base != "/admin/mail" {
		t.Fatalf("基址应原样带出，实际 %q", pg.Base)
	}
	if !pg.DropLimit {
		t.Error("账号页每页固定，链接不得带 limit（DropLimit 必须为 true）")
	}
	if !templates.PagerOf(pg).HasPagination() || !templates.PagerOf(pg).HasNext() || templates.PagerOf(pg).HasPrev() {
		t.Fatalf("45 条 / 每页 20：应成立且只有下一页，实际 %+v", pg)
	}
	if pg.Total != 45 || pg.Page != 1 || pg.Limit != mailListPageSize {
		t.Fatalf("事实算错：total=%d page=%d limit=%d", pg.Total, pg.Page, pg.Limit)
	}
}

func TestMailListPaginationFactsSinglePage(t *testing.T) {
	for _, total := range []int64{0, 1, int64(mailListPageSize)} {
		pg := mailListPaginationFacts(total, 1, "page", "/admin/mail/templates")
		if templates.PagerOf(pg).HasPagination() {
			t.Fatalf("total=%d 不应成立分页，实际 %+v", total, pg)
		}
	}
}

func TestMailContactsClampPage(t *testing.T) {
	cases := []struct {
		name  string
		page  int
		total int64
		want  int
	}{
		{"在范围内 → 不动", 2, 120, 2},
		{"越界 → 收敛到最大页", 3, 10, 1},
		{"空数据 → 收敛到 1（不显示空页码）", 7, 0, 1},
		{"页码非法（负数）→ 1", -2, 120, 1},
		{"刚好整除的边界：total=100/每页 50 → 第 2 页有效", 2, 100, 2},
		{"刚好整除的边界：第 3 页越界", 3, 100, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mailContactsClampPage(c.page, c.total); got != c.want {
				t.Fatalf("收敛后页码 %d，期望 %d", got, c.want)
			}
		})
	}
}

func TestMailCampaignsClampPage(t *testing.T) {
	cases := []struct {
		page  int
		total int64
		want  int
	}{
		{1, 0, 1},
		{4, 51, 2},
		{2, 51, 2},
		{-1, 51, 1},
	}
	for _, c := range cases {
		if got := mailCampaignsClampPage(c.page, c.total); got != c.want {
			t.Fatalf("page=%d total=%d 收敛后 %d，期望 %d", c.page, c.total, got, c.want)
		}
	}
}

func TestMailPaginationFiltersStayInBase(t *testing.T) {
	base := reqparams.FilterBaseURL(mailContactsPath, map[string]string{
		"keyword": "vip@example.com", "status": "subscribed",
	})
	pg := pagination.Page{Total: int64(mailContactsPageSize) + 1, Page: 1, Limit: mailContactsPageSize}
	pg.Base = base
	if pg.Base != base {
		t.Fatalf("Base 应原样带出，实际 %q", pg.Base)
	}
	for _, want := range []string{"keyword=vip%40example.com", "status=subscribed"} {
		if !strings.Contains(pg.Base, want) {
			t.Errorf("基址 %q 缺少筛选 %q —— 翻页会回到未筛选的全量", pg.Base, want)
		}
	}
	if !templates.PagerOf(pg).HasPagination() || !templates.PagerOf(pg).HasNext() {
		t.Fatalf("每页 %d 条、共 %d 条应超一页，实际 %+v", mailContactsPageSize, mailContactsPageSize+1, pg)
	}

	if pg.DropLimit {
		t.Error("联系人页链接应带 limit（DropLimit 默认 false）")
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
