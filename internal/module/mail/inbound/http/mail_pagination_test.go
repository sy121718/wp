package mailhttp

// mail_pagination_test.go — 邮箱后台列表分页的纯逻辑守卫。
//
// 拆成六页后每页各自一张表：分页参数不再互带（账号页的 account_page 不会出现在
// 其它页的分页链接里），页码收敛也退化成单表收敛。数据库负责计数与取页。
// 单页 / 空数据时不出现分页条。
//
// 端到端（真页面 + 真模板 + 真 DB）在 public/test/mail/feature/mail_page_pagination_test.go。

import (
	"strings"
	"testing"

	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
)

// TestMailListPaginationFacts 列表页分页事实：页码参数名归域内、链接不带 limit。
//
// 迁移后 handler 只给事实（Base / PageParam / KeepLimit），链接由 partial 用它们生成 ——
// 所以这里钉的是**给模板的输入**：参数名、基址、是否带 limit、分页是否成立。
// 渲染出来的 href 字节由 internal/templates 的 facts 矩阵与 mail 的 E2E 覆盖。
func TestMailListPaginationFacts(t *testing.T) {
	pg := mailListPaginationFacts(45, 1, "account_page", "/admin/mail")
	if pg.PageParam != "account_page" {
		t.Fatalf("页码参数名应归域内 account_page，实际 %q", pg.PageParam)
	}
	if pg.Base != "/admin/mail" {
		t.Fatalf("基址应原样带出，实际 %q", pg.Base)
	}
	if pg.KeepLimit {
		t.Error("账号页每页固定，链接不得带 limit（KeepLimit 必须为 false）")
	}
	if !pg.HasPagination() || !pg.HasNext() || pg.HasPrev() {
		t.Fatalf("45 条 / 每页 20：应成立且只有下一页，实际 %+v", pg)
	}
	if pg.Pages != 3 || pg.Page != 1 || pg.Start != 1 || pg.End != 20 {
		t.Fatalf("事实算错：pages=%d page=%d start=%d end=%d", pg.Pages, pg.Page, pg.Start, pg.End)
	}
}

// TestMailListPaginationFactsSinglePage 单页 / 空数据不成立 —— handler 据此**不注入**键，
// 与迁移前 TemplateKeys 给空 map 的效果一致（模板侧 isset 守卫）。
func TestMailListPaginationFactsSinglePage(t *testing.T) {
	for _, total := range []int64{0, 1, int64(mailListPageSize)} {
		if pg := mailListPaginationFacts(total, 1, "page", "/admin/mail/templates"); pg.HasPagination() {
			t.Fatalf("total=%d 不应成立分页，实际 %+v", total, pg)
		}
	}
}

// TestMailContactsClampPage 单表页码收敛（越界收敛到最大页，空数据收敛到 1）。
//
// 为什么必须收敛：mail 服务端分页返回的是「空列表 + 真实 total」，
// 不收敛就会把「有 51 个联系人、只是页码落到第 9 页」渲染成「没有匹配的联系人」。
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

// TestMailCampaignsClampPage 活动页的收敛与联系人页同规则（分页语义一致）。
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

// TestMailPaginationFiltersStayInBase 筛选参数必须留在**基址**里：
// 链接由 partial 用 Base 拼，Base 丢筛选就等于翻页回全量（而人以为自己还在筛选里）。
//
// 迁移前这一条靠断言 BuildPagination 产出的链接字节；迁移后 handler 与模板分工变了，
// 断言的落点随之变成「注入给模板的 Base」，href 字节由 internal/templates 的矩阵覆盖。
func TestMailPaginationFiltersStayInBase(t *testing.T) {
	base := reqparams.FilterBaseURL(mailContactsPath, map[string]string{
		"keyword": "vip@example.com", "status": "subscribed",
	})
	pg := pagination.Facts(int64(mailContactsPageSize)+1, 1, mailContactsPageSize)
	pg.Base = base
	if pg.Base != base {
		t.Fatalf("Base 应原样带出，实际 %q", pg.Base)
	}
	for _, want := range []string{"keyword=vip%40example.com", "status=subscribed"} {
		if !strings.Contains(pg.Base, want) {
			t.Errorf("基址 %q 缺少筛选 %q —— 翻页会回到未筛选的全量", pg.Base, want)
		}
	}
	if !pg.HasPagination() || !pg.HasNext() {
		t.Fatalf("每页 %d 条、共 %d 条应超一页，实际 %+v", mailContactsPageSize, mailContactsPageSize+1, pg)
	}
	// 联系人页保留 limit（与账号页相反：那两页每页固定 20 条，从链接里去掉）。
	if !pg.KeepLimit {
		t.Error("联系人页链接应带 limit（KeepLimit 默认 true）")
	}
}

// itoaTest 小整数转字符串（避免为一条断言引入 strconv 依赖的噪声）。
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
