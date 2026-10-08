package templates

// admin_list_pagination_render_test.go — 列表页分页条的渲染级守卫（审计 02-L §2 P1-13 / P1-14）。
//
// 分页条本身是 `admin/partials/pagination.html`：它只读两个键（PaginationInfo / PaginationLinks），
// **单页或空数据时不渲染**（handler 侧的 shell.BuildPagination 在 total 不超过一页时返回 nil，
// TemplateKeys 给出空 map）。所以「有没有分页」这件事有两半：handler 注入的键名对不对、
// 模板消费的位置与传参对不对。本文件钉住后一半。
//
// 三条判据：
//  1. 无分页数据（键缺失）时**不出现** `.pagination` —— 单页列表上挂一条「上一页 / 下一页」是噪声；
//  2. 有分页数据时出现在**正确的那张表下面**（拆页前一个页面有两张表、各自渲染自己的分页数据，
//     共用一组键名会让后算的那张表覆盖前一张；那个结构已随 issue #37 拆页消失，但
//     「键名与渲染位置一一对应」这条判据要留着守 —— 对错位置同样是用户看不见分页）；
//  3. 分页链接保留筛选参数（翻页不能把用户的筛选条件丢掉）。
//
// 为什么这里用本地的 `pageLinkProbe` 而不是 shell.PageLink：`internal/shell` 反向 import 了
// 本包（shell.TranslateFor → templates.TranslateFunc），本包再 import shell 就成环。
// 探针的字段名与 shell.PageLink 逐字相同（Label / URL / Active / Disabled）——
// 那边一旦改名，Jet 取字段会直接报 `no field or method Label`，本文件立刻变红。
// 真实类型在端到端用例里覆盖：public/test/mail/feature 的页面分页测试走真 handler + 真模板。

import (
	"html"
	"strings"
	"testing"
)

// plainPaginationHrefs 摘出输出里的分页链接（断言失败时给出可读的实际值，而不是整页 HTML）。
func plainPaginationHrefs(plain string) string {
	var hrefs []string
	for _, part := range strings.Split(plain, `href="`) {
		end := strings.Index(part, `"`)
		if end <= 0 {
			continue
		}
		if href := part[:end]; strings.Contains(href, "page=") {
			hrefs = append(hrefs, href)
		}
	}
	return strings.Join(hrefs, " | ")
}

// pageLinkProbe 与 shell.PageLink 同形状的渲染探针（见文件头注释的成环说明）。
type pageLinkProbe struct {
	Label    string
	URL      string
	Active   bool
	Disabled bool
}

// paginationKeys 造一组「共 N 条 + 两个页码」的分页数据，键名与 handler 注入的完全一致。
func paginationKeys(info string, links []pageLinkProbe) map[string]any {
	return map[string]any{"PaginationInfo": info, "PaginationLinks": links}
}

// mailProbeLinks 一组指向 /admin/mail 的分页链接（用于 mail 页两张表的探针数据）。
func mailProbeLinks(page int) []pageLinkProbe {
	next := "/admin/mail?page=" + itoa(page) + "&limit=20"
	return []pageLinkProbe{
		{Label: "上一页", Disabled: page == 1},
		{Label: itoa(page), URL: "/admin/mail?page=" + itoa(page) + "&limit=20", Active: true},
		{Label: itoa(page + 1), URL: next},
		{Label: "下一页", URL: next},
	}
}

// itoa 数字转字符串（用例里只有很小的页码，不需要 strconv 的错误分支）。
func itoa(n int) string {
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

// TestAdminListPaginationRenders 分页条只在有分页数据时渲染，且落在正确的那张表下面。
func TestAdminListPaginationRenders(t *testing.T) {
	t.Run("mail/无分页数据→不渲染分页条", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail", map[string]any{
			"Accounts": []any{}, "Templates": []any{},
		})
		if strings.Contains(out, `class="pagination"`) {
			t.Error("mail：没有分页数据时不应渲染分页条")
		}
	})

	// 拆页后每页只有一张表：账号表的分页数据落在账号页自己的分页条上，
	// 不再有「两表共用一组键名互相覆盖」的风险（拆页前那正是本用例守的东西）。
	t.Run("mail/账号表的分页条", func(t *testing.T) {
		data := map[string]any{"Accounts": []any{}}
		for k, v := range paginationKeys("共 41 条，第 1-20 条", mailProbeLinks(1)) {
			data[k] = v
		}
		out := renderAdminEmptyProbe(t, "admin/mail/mail", data)
		if got := strings.Count(out, `class="pagination"`); got != 1 {
			t.Fatalf("mail：账号表有分页数据时应渲染 1 条分页条，实际 %d 条", got)
		}
		if !strings.Contains(out, "共 41 条，第 1-20 条") {
			t.Error("mail：分页信息未渲染")
		}
	})

	t.Run("mail_contacts/分页条且保留筛选", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_contacts", marketingProbeData(map[string]any{
			"Contacts": []any{},
			"Keyword":  "vip@example.com", "Status": "subscribed",
			"PaginationInfo": "共 120 条，第 51-100 条",
			// 分页条里的当前页渲染成 <span>（没有 href），所以断言筛选参数必须看**非当前页**
			// 的链接 —— 这正好是用户点「翻页」时走的那条。
			"PaginationLinks": []pageLinkProbe{
				{Label: "1", URL: "/admin/mail/contacts?keyword=vip%40example.com&status=subscribed&page=1"},
				{Label: "2", URL: "/admin/mail/contacts?keyword=vip%40example.com&status=subscribed&page=2", Active: true},
			},
		}))
		if got := strings.Count(out, `class="pagination"`); got != 1 {
			t.Fatalf("mail_contacts：分页数据应渲染 1 条分页条，实际 %d 条", got)
		}
		// 翻页链接必须带筛选参数，否则点下一页就回到未筛选的全量列表。
		// 先反转义再断言：Jet 会把 href 里的 `&` 转成 `&amp;`（合法的 HTML），
		// 直接比对原始输出会把「转义」误判成「丢了参数」。
		plain := html.UnescapeString(out)
		if !strings.Contains(plain, "keyword=vip%40example.com") || !strings.Contains(plain, "status=subscribed") {
			t.Errorf("mail_contacts：分页链接丢了筛选参数（翻页会回到未筛选状态）：%s", plainPaginationHrefs(plain))
		}
		if !strings.Contains(out, "共 120 条，第 51-100 条") {
			t.Error("mail_contacts：分页信息未渲染")
		}
	})

	t.Run("mail_campaign/收件人明细分页条", func(t *testing.T) {
		base := map[string]any{
			"Err": "",
			"R": map[string]any{
				"Campaign": map[string]any{"Name": "九月活动", "Status": "sent", "Subject": "上新"},
				"Links":    []any{}, "Recipients": []any{}, "Total": 120,
			},
		}
		// 单页（没有分页数据键）→ 不渲染。
		single := renderAdminEmptyProbe(t, "admin/mail/mail_campaign", base)
		if strings.Contains(single, `class="pagination"`) {
			t.Error("mail_campaign：没有分页数据时不应渲染分页条（此前的「第 N 页」文本已删除）")
		}
		for k, v := range paginationKeys("共 120 条，第 51-100 条", []pageLinkProbe{
			{Label: "1", URL: "/admin/mail/campaign?id=7&page=1&limit=50"},
			{Label: "2", URL: "/admin/mail/campaign?id=7&page=2&limit=50", Active: true},
		}) {
			base[k] = v
		}
		out := renderAdminEmptyProbe(t, "admin/mail/mail_campaign", base)
		if got := strings.Count(out, `class="pagination"`); got != 1 {
			t.Fatalf("mail_campaign：应收件人明细下有 1 条分页条，实际 %d 条", got)
		}
		// 翻页链接必须带活动 id，否则翻到第 2 页就丢掉了「在看哪条活动」（同上的反转义理由）。
		// 当前页渲染成 <span>，所以看第 1 页那条链接（用户点它回到第 1 页时走的路径）。
		if !strings.Contains(html.UnescapeString(out), "/admin/mail/campaign?id=7&page=1&limit=50") {
			t.Error("mail_campaign：分页链接丢了活动 id")
		}
	})
}
