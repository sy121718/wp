package mailhttp

// mail_pagination_test.go — 邮箱后台列表分页的纯逻辑守卫。
//
// 邮箱设置页两张表各用独立页码，数据库负责计数与取页；营销页仍用共享页码。
// 单页 / 空数据时不出现分页条，翻页链接需保留另一张表的状态。
//
// 端到端（真页面 + 真模板 + 真 DB）在 public/test/mail/feature/mail_page_pagination_test.go。

import (
	"strings"
	"testing"

	"go_wp/internal/web/shell"
)

// TestMailListPaginationLinks 两张列表分别翻页时保留另一页的状态。
func TestMailListPaginationLinks(t *testing.T) {
	for _, tc := range []struct {
		name, param, other string
	}{
		{"账号翻页保留模板页", "account_page", "template_page=3"},
		{"模板翻页保留账号页", "template_page", "account_page=3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := mailListPagination(45, 1, tc.param, "/admin/mail", 3, nil)
			links := keys["PaginationLinks"].([]shell.PageLink)
			for _, link := range links {
				if strings.Contains(link.URL, tc.param+"=2") {
					if !strings.Contains(link.URL, tc.other) || strings.Contains(link.URL, "page=2&limit=") {
						t.Fatalf("翻页链接丢失另一页参数或泄漏共享页码：%s", link.URL)
					}
					return
				}
			}
			t.Fatalf("缺少第二页链接：%+v", links)
		})
	}
}

// TestMailMarketingClampPage 两张表共用页码时的收敛（取两表中更小的最大页）。
func TestMailMarketingClampPage(t *testing.T) {
	cases := []struct {
		name   string
		page   int
		totals []int64
		want   int
	}{
		{"两表都在范围内 → 不动", 2, []int64{120, 60}, 2},
		{"联系人有一页、活动也有一页 → 收敛到 1", 3, []int64{10, 5}, 1},
		{"只按页数少的那张表收敛", 3, []int64{200, 5}, 1},
		{"活动为空（total=0）→ 只按联系人收敛", 3, []int64{60, 0}, 2},
		{"两表都空 → 收敛到 1（不显示空页码）", 7, []int64{0, 0}, 1},
		{"页码非法（负数）→ 1", -2, []int64{120, 60}, 1},
		{"刚好整除的边界：total=100/每页 50 → 第 2 页有效", 2, []int64{100, 100}, 2},
		{"刚好整除的边界：第 3 页越界", 3, []int64{100, 100}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mailMarketingClampPage(c.page, c.totals...); got != c.want {
				t.Fatalf("收敛后页码 %d，期望 %d", got, c.want)
			}
		})
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
		for _, total := range []int64{0, 1, int64(mailMarketingPageSize)} {
			keys := shell.BuildPagination(total, 1, mailMarketingPageSize, "/admin/mail/marketing", nil).TemplateKeys()
			if len(keys) != 0 {
				t.Fatalf("total=%d 不应注入分页键，实际 %v", total, keys)
			}
		}
	})

	t.Run("超一页 → 注入页码，且链接保留筛选", func(t *testing.T) {
		base := shell.FilterBaseURL("/admin/mail/marketing", map[string]string{
			"keyword": "vip@example.com", "status": "subscribed",
		})
		keys := shell.BuildPagination(int64(mailMarketingPageSize)+1, 1, mailMarketingPageSize, base, nil).TemplateKeys()
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
		for _, want := range []string{"keyword=vip%40example.com", "status=subscribed", "page=2", "limit=" + itoaTest(mailMarketingPageSize)} {
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
