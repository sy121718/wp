package templates

// admin_mail_pagination_facts_test.go — task-105（P3-B1）：mail 六页分页迁移到事实层的
// **旧/新等价与追认修复**证据。真实 Jet（磁盘模板 + 生产 partial），不 mock 渲染。
//
// 三组判据：
//  1. 四个「handler 直接调 BuildPagination」的页面（自动化运行实例 / 群发活动 / 活动收件人明细 /
//     联系人）：把**迁移前的两键**（旧 handler 的真实产出 = BuildPagination + TemplateKeys）
//     与**迁移后的事实键**分别喂给同一个 partial，渲染结果必须**逐字节相同**。
//  2. 账号页（helper，param=account_page，无 limit）：等价性由 P2 套件锚定在**迁移前冻结的
//     P0a-6 golden** 上（admin_pagination_dual_shape_p2_test.go 的 T1/T2），此处不重复冻结。
//  3. 模板页（helper，param=page）：**有意修复** —— 旧 helper 的 url 改写把刚设好的页码删掉，
//     链接退化成不带 ?page 的基址（翻页失效）；事实层渲染必须给出 ?page=N。
//
// 单页 / 缺键（= 取数失败档：mail 的四个 ErrData helper 都不注入分页键）→ 六页都不得出现分页条。

import (
	"html"
	"strings"
	"testing"

	"go_wp/pkg/pagination"
)

// mailPaginationFacts 造一页的分页事实，字段与 handler 注入的同形。
func mailPaginationFacts(total int64, page, size int, base, param string, keepLimit bool) pagination.Page {
	pg := pagination.Facts(total, page, size)
	pg.Base, pg.PageParam, pg.KeepLimit = base, param, keepLimit
	return pg
}

// renderPaginationPartial 用给定数据渲染分页片段本体（真实 Jet）。
func renderPaginationPartial(t *testing.T, extra map[string]any) string {
	t.Helper()
	data := adminShellData()
	for k, v := range extra {
		data[k] = v
	}
	out, err := render(t, newAdminTestSet(), "admin/partials/pagination.html", data)
	if err != nil {
		t.Fatalf("渲染分页片段失败: %v", err)
	}
	return out
}

// TestMailPaginationFactsMatchLegacyBytes 四个直接消费者的旧/新渲染逐字节等价。
//
// 旧数据不是手搓的：它就是旧 handler 的那一行 ——
// `pagination.BuildPagination(total, page, size, base, tr).TemplateKeys()`。
func TestMailPaginationFactsMatchLegacyBytes(t *testing.T) {
	cases := []struct {
		name  string
		total int64
		page  int
		size  int
		base  string
	}{
		{"automation_runs", 120, 3, 50, "/admin/mail/automation/runs?automationId=7&runStatus=failed"},
		{"campaigns", 120, 3, 50, "/admin/mail/campaigns"},
		{"campaign_report", 120, 3, 50, "/admin/mail/campaign?id=7"},
		{"contacts", 120, 3, 50, "/admin/mail/contacts?keyword=vip%40example.com&status=subscribed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := pagination.BuildPagination(c.total, c.page, c.size, c.base, nil)
			if before == nil {
				t.Fatal("前置条件不成立：该形态应产生分页数据")
			}
			legacy := renderPaginationPartial(t, before.TemplateKeys())
			facts := renderPaginationPartial(t, map[string]any{
				"Pagination": mailPaginationFacts(c.total, c.page, c.size, c.base, "page", true),
			})
			if legacy != facts {
				off := p2FirstDiff(legacy, facts)
				t.Fatalf("%s：事实层渲染与迁移前字节不同（首个差异：%s）", c.name, off)
			}
			// 反面：基址里的筛选/标识参数必须在链接里（丢了就等于翻页回全量）。
			if strings.Contains(c.base, "?") {
				baseQuery := c.base[strings.Index(c.base, "?")+1:]
				for _, kv := range strings.Split(baseQuery, "&") {
					if !strings.Contains(html.UnescapeString(legacy), kv) {
						t.Errorf("%s：分页链接丢了基址参数 %q", c.name, kv)
					}
				}
			}
		})
	}
}

// TestMailTemplatesPagePaginationLinksCarryPageParam 模板页的**追认修复**（lead 裁定 b）：
// 旧 helper 在 param=="page" 时先 q.Set("page", 自己) 再无条件 q.Del("page")，链接退化成
// 不带 ?page 的基址；事实层必须给出 ?page=N，否则「下一页」永远回到第 1 页。
func TestMailTemplatesPagePaginationLinksCarryPageParam(t *testing.T) {
	out := renderPaginationPartial(t, map[string]any{
		"Pagination": mailPaginationFacts(45, 2, 20, "/admin/mail/templates", "page", false),
	})
	plain := html.UnescapeString(out)
	for _, want := range []string{`href="/admin/mail/templates?page=1"`, `href="/admin/mail/templates?page=3"`} {
		if !strings.Contains(plain, want) {
			t.Fatalf("模板页翻页链接应带页码参数（追认修复）：缺 %s，实际 %s", want, plainPaginationHrefs(plain))
		}
	}
	if strings.Contains(plain, `href="/admin/mail/templates"`) {
		t.Errorf("模板页不应再有「不带 ?page 的基址链接」（那是修复前的坏形态）：%s", plainPaginationHrefs(plain))
	}
	if strings.Contains(plain, "limit=") {
		t.Errorf("模板页每页固定，链接不得带 limit：%s", plainPaginationHrefs(plain))
	}
}

// TestMailPagesPaginateOnlyWithFacts 六页的真实 Jet 探针：
//   - 注入事实且超过一页 → 恰好一条分页条 + 信息行由 partial 算出；
//   - 不注入（= 取数失败档，四个 ErrData helper 都不注入分页键）或单页 → 不出现分页条。
func TestMailPagesPaginateOnlyWithFacts(t *testing.T) {
	pages := []struct {
		name string
		tpl  string
		data map[string]any
		// 该页每页条数与基址（与 handler 注入一致）
		size int
		base string
		// 断言信息行（partial 按语言 + 事实算出来的一句）
		info  string
		total int64
	}{
		{
			name: "mail/账号页", tpl: "admin/mail/mail",
			data: map[string]any{"Accounts": []any{}},
			size: 20, base: "/admin/mail", info: "共 41 条，第 21-40 条", total: 41,
		},
		{
			name: "mail_templates/模板页", tpl: "admin/mail/mail_templates",
			data: map[string]any{"Templates": []any{}},
			size: 20, base: "/admin/mail/templates", info: "共 41 条，第 21-40 条", total: 41,
		},
		{
			name: "mail_automation_runs/运行实例", tpl: "admin/mail/mail_automation_runs",
			data: map[string]any{
				// 取数失败档走同一模板（handler 注入 Err），空列表 + 空筛选即可渲染。
				"Runs": []any{}, "Counts": map[string]any{}, "RunTotal": int64(0),
				"FilterID": "", "FilterRun": "", "CurrentNode": "",
			},
			size: 50, base: "/admin/mail/automation/runs?automationId=7", info: "共 120 条，第 51-100 条", total: 120,
		},
		{
			name: "mail_campaigns/群发活动", tpl: "admin/mail/mail_campaigns",
			data: marketingProbeData(map[string]any{}),
			size: 50, base: "/admin/mail/campaigns", info: "共 120 条，第 51-100 条", total: 120,
		},
	}
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			// 单页 / 缺键（失败档）→ 不渲染。缺键这一支就是失败档的真实形态。
			for _, label := range []string{"缺键（失败档）", "单页"} {
				data := withKeys(map[string]any{}, p.data)
				if label == "单页" {
					data["Pagination"] = mailPaginationFacts(int64(p.size), 1, p.size, p.base, "page", true)
				}
				out := renderAdminEmptyProbe(t, p.tpl, data)
				if strings.Contains(out, `class="pagination"`) {
					t.Errorf("%s/%s：不应渲染分页条", p.name, label)
				}
			}
			data := withKeys(map[string]any{"Pagination": mailPaginationFacts(p.total, 2, p.size, p.base, "page", true)}, p.data)
			out := renderAdminEmptyProbe(t, p.tpl, data)
			if got := strings.Count(out, `class="pagination"`); got != 1 {
				t.Fatalf("%s：有分页事实时应渲染 1 条分页条，实际 %d 条", p.name, got)
			}
			if !strings.Contains(out, p.info) {
				t.Errorf("%s：信息行应由 partial 按事实算出（期望含 %q）", p.name, p.info)
			}
		})
	}
}
