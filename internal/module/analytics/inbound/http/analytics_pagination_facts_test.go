package analyticshttp

import (
	"html"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

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

func TestAnalyticsPagePaginationIsFactsDriven(t *testing.T) {
	body := analyticsPageBody(t, "/admin/analytics?project=p-1&page=2", analyticsSummaryWithPages())
	nav := anPaginationNav(t, body)

	base := "/admin/analytics?" + url.Values{"project": {"p-1"}, "view": {analyticsViewPaths}}.Encode()

	want := []string{
		base + "&page=1&limit=20",
		base + "&page=3&limit=20",
	}
	sort.Strings(want)
	if got := anPaginationHrefs(nav); !reflect.DeepEqual(got, want) {
		t.Errorf("分页链接字节不符\n实际: %q\n期望: %q", got, want)
	}

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

	for _, dead := range []string{"PaginationInfo", "PaginationLinks"} {
		if strings.Contains(body, dead) {
			t.Errorf("页面里不应再出现旧键 %q", dead)
		}
	}
}

func TestAnalyticsPageSinglePageRendersNoPagination(t *testing.T) {
	sum := analyticsSummaryWithPages()
	sum.PathTotal = 5
	body := analyticsPageBody(t, "/admin/analytics?project=p-1", sum)
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("单页不该渲染分页条（PathTotal=5 / limit=20）")
	}
}
