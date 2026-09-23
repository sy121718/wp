package mailhttp

// mail_pagination_test.go — 邮箱后台三处分页的纯逻辑守卫（审计 02-L §2 P1-13）。
//
// 三处缺口各不相同，测试也分开钉：
//  1. mail 页（发信账号 + 邮件模板）：契约只返回 []Item（没有 total / limit / offset），
//     分页在 handler 侧做 —— 这里钉住切片与**页码收敛**（page=999 时表格不能是空的）；
//  2. mail_marketing 页：两个列表共用 ?page=，而服务端分页不收敛 —— 这里钉住收敛函数；
//  3. 分页条本身：单页 / 空数据时不能出现（否则单页列表上挂一条「上一页 / 下一页」），
//     超一页时链接必须**保留筛选参数**（翻页把用户的筛选丢掉是这类改动最常见的回归）。
//
// 端到端（真页面 + 真模板 + 真 DB）在 public/test/mail/feature/mail_page_pagination_test.go。

import (
	"strings"
	"testing"

	"go_wp/internal/web/shell"
)

// TestMailListPageSlice 切片与页码收敛（含越界页、空列表、size 缺省）。
func TestMailListPageSlice(t *testing.T) {
	all := make([]int, 45) // 45 条 / 每页 20 → 3 页（20 / 20 / 5）
	for i := range all {
		all[i] = i + 1
	}

	cases := []struct {
		name      string
		page      int
		size      int
		wantFirst int // 期望首页元素（0 表示空切片）
		wantLen   int
		wantPage  int
	}{
		{"第 1 页", 1, 20, 1, 20, 1},
		{"第 3 页（最后一页只 5 条）", 3, 20, 41, 5, 3},
		{"页码越界 → 收敛到最后一页而不是空表", 999, 20, 41, 5, 3},
		{"page 非法（0）→ 按第 1 页", 0, 20, 1, 20, 1},
		{"size 非法（0）→ 用默认值", 1, 0, 1, mailListPageSize, 1},
		{"空列表 → 空切片、页码收敛到 1", 1, 20, 0, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := all
			if c.wantLen == 0 {
				in = nil
			}
			rows, page := mailListPageSlice(in, c.page, c.size)
			if len(rows) != c.wantLen {
				t.Fatalf("切出 %d 行，期望 %d 行", len(rows), c.wantLen)
			}
			if page != c.wantPage {
				t.Fatalf("收敛后页码 %d，期望 %d", page, c.wantPage)
			}
			if c.wantFirst != 0 && rows[0] != c.wantFirst {
				t.Fatalf("首页元素 %d，期望 %d", rows[0], c.wantFirst)
			}
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
