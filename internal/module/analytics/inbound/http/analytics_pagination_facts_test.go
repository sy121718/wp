package analyticshttp

// analytics_pagination_facts_test.go — task-116（P3-B3）：访问统计页的分页消费者迁到
// pkg/pagination 事实层后的 handler 级判据。
//
// 判据是「渲染字节不变」：迁移前 handler 用 BuildPagination 预制成品（信息行句子 + 链接 +
// 选中态），迁移后只交事实（pagination.Page），句子与链接由 admin/partials/pagination.html
// 用通用模板函数渲染。这里的期望值用 url.Values **独立**拼出，不拿生产 helper 的结果当期望。
//
// 本页独有的陷阱：分页链接必须固定带 view=paths（分页条物理上只属于「按路径」面板），
// 见 analytics_page_render_test.go 的 TestAnalyticsPaginationLinksCarryViewPaths。

import (
	"html"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// anPaginationNav 取页面里的分页条整块（`<nav class="pagination"` 到 `</nav>`）。
func anPaginationNav(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<nav class="pagination"`)
	if i < 0 {
		t.Fatalf("页面里没有分页条（PathTotal 45 / limit 20 ⇒ 应有 3 页）")
	}
	j := strings.Index(body[i:], "</nav>")
	if j < 0 {
		t.Fatalf("分页条没有闭合")
	}
	return body[i : i+j+len("</nav>")]
}

// anPaginationHrefs 分页条里全部 href（HTML 还原、去重、排序）。
//
// 去重是因为「上一页」与页码 1 指向同一个 URL —— 重复并不说明有问题，
// 所以重数由下面的整元素断言（上一页/下一页按钮字节）单独钉住。
func anPaginationHrefs(nav string) []string {
	re := regexp.MustCompile(`href="([^"]*)"`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(nav, -1) {
		u := html.UnescapeString(m[1])
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// TestAnalyticsPagePaginationIsFactsDriven handler 交事实、模板渲染：链接字节逐条一致。
func TestAnalyticsPagePaginationIsFactsDriven(t *testing.T) {
	body := analyticsPageBody(t, "/admin/analytics?project=p-1&page=2", analyticsSummaryWithPages())
	nav := anPaginationNav(t, body)

	// 期望基址独立拼出：project 取请求值、view 固定 paths、from/to 为空 ⇒ 不进基址。
	base := "/admin/analytics?" + url.Values{"project": {"p-1"}, "view": {analyticsViewPaths}}.Encode()
	// 当前页（2）渲染成 is-active 的 span，**不是**链接 —— 所以带 href 的只有 1 与 3。
	want := []string{
		base + "&page=1&limit=20",
		base + "&page=3&limit=20",
	}
	sort.Strings(want)
	if got := anPaginationHrefs(nav); !reflect.DeepEqual(got, want) {
		t.Errorf("分页链接字节不符\n实际: %q\n期望: %q", got, want)
	}
	// 整元素字节：信息行由事实算出；当前页是 is-active span（不是链接）；
	// 上一页/下一页是链接且 URL 与上面同一口径（含 HTML 转义形态）。
	for _, want := range []string{
		`<div class="pagination-info">共 45 条，第 21-40 条</div>`,
		`<span class="pagination-btn is-active">2</span>`,
		`<a class="pagination-btn" href="` + html.EscapeString(base+"&page=1&limit=20") + `">上一页</a>`,
		`<a class="pagination-btn" href="` + html.EscapeString(base+"&page=3&limit=20") + `">下一页</a>`,
	} {
		if !strings.Contains(nav, want) {
			t.Errorf("分页条缺少整元素 %s\n实际: %s", want, nav)
		}
	}
	// 旧两键已退出本页：留着它们等于给「顺手改回旧路径」留后门。
	for _, dead := range []string{"PaginationInfo", "PaginationLinks"} {
		if strings.Contains(body, dead) {
			t.Errorf("页面里不应再出现旧键 %q", dead)
		}
	}
}

// TestAnalyticsPageSinglePageRendersNoPagination 单页不渲染分页条（total=5 / limit=20）。
func TestAnalyticsPageSinglePageRendersNoPagination(t *testing.T) {
	sum := analyticsSummaryWithPages()
	sum.PathTotal = 5
	body := analyticsPageBody(t, "/admin/analytics?project=p-1", sum)
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("单页不该渲染分页条（PathTotal=5 / limit=20）")
	}
}
