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

	"go_wp/internal/web/shell"
)

// TestMailListPaginationLinks 列表翻页链接只带自己的页码参数。
func TestMailListPaginationLinks(t *testing.T) {
	keys := mailListPagination(45, 1, "account_page", "/admin/mail", nil)
	links, ok := keys["PaginationLinks"].([]shell.PageLink)
	if !ok || len(links) == 0 {
		t.Fatalf("应注入分页链接，实际 %#v", keys)
	}
	for _, link := range links {
		if !strings.Contains(link.URL, "account_page=2") {
			continue
		}
		// 拆页前两张表共用 ?page=，链接里必须互带另一张表的页码；
		// 拆页后每页只有一张表，链接里既不该有共享 page（会被误读成另一页的页码），
		// 也不该出现另一页的参数（那会把用户送到一个不存在的筛选状态）。
		if strings.Contains(link.URL, "template_page") || strings.Contains(link.URL, "limit=") {
			t.Fatalf("翻页链接泄漏了无关参数：%s", link.URL)
		}
		return
	}
	t.Fatalf("缺少第二页链接：%+v", links)
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

// TestMailPaginationOnlyBeyondOnePage 分页条只在超一页时出现，且链接保留筛选参数。
//
// 这一条钉的是「handler 侧注入给模板的键」的行为契约（模板如何消费见
// internal/templates/admin_list_pagination_render_test.go）：
// TemplateKeys 在无分页时给出空 map，模板据此不渲染 —— 谁把它改成「总是注入」，
// 单页列表上就会出现一条只有一个「1」的分页条。
func TestMailPaginationOnlyBeyondOnePage(t *testing.T) {
	t.Run("空数据 / 单页 → 不注入", func(t *testing.T) {
		for _, total := range []int64{0, 1, int64(mailContactsPageSize)} {
			keys := shell.BuildPagination(total, 1, mailContactsPageSize, "/admin/mail/contacts", nil).TemplateKeys()
			if len(keys) != 0 {
				t.Fatalf("total=%d 不应注入分页键，实际 %v", total, keys)
			}
		}
	})

	t.Run("超一页 → 注入页码，且链接保留筛选", func(t *testing.T) {
		base := shell.FilterBaseURL("/admin/mail/contacts", map[string]string{
			"keyword": "vip@example.com", "status": "subscribed",
		})
		keys := shell.BuildPagination(int64(mailContactsPageSize)+1, 1, mailContactsPageSize, base, nil).TemplateKeys()
		links, ok := keys["PaginationLinks"].([]shell.PageLink)
		if !ok || len(links) == 0 {
			t.Fatalf("超一页应注入页码链接，实际 %#v", keys)
		}
		// 当前页是 span（没有 URL），所以检查非当前页的链接 —— 那才是「翻页」走的路径。
		var href string
		for _, l := range links {
			if !l.Active && !l.Disabled && l.URL != "" {
				href = l.URL
				break
			}
		}
		if href == "" {
			t.Fatalf("应存在可点的页码链接，实际 %+v", links)
		}
		for _, want := range []string{"keyword=vip%40example.com", "status=subscribed", "page=2", "limit=" + itoaTest(mailContactsPageSize)} {
			if !strings.Contains(href, want) {
				t.Errorf("翻页链接 %q 缺少 %q —— 翻页会丢筛选或丢每页条数", href, want)
			}
		}
	})
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
