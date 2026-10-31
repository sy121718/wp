package commenthttp

import (
	"html"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	commentdto "go_wp/internal/module/comment/dto"
	projectdto "go_wp/internal/module/project/dto"
)

func cmPaginationNav(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<nav class="pagination"`)
	if i < 0 {
		t.Fatalf("页面里没有分页条（Total 45 / 每页 20 ⇒ 应有 3 页）")
	}
	j := strings.Index(body[i:], "</nav>")
	if j < 0 {
		t.Fatalf("分页条没有闭合")
	}
	return body[i : i+j+len("</nav>")]
}

func cmPaginationHrefs(nav string) []string {
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

func TestCommentsPagePaginationIsFactsDriven(t *testing.T) {
	svc := &stubCommentSvc{res: &commentdto.AdminListResp{Total: 45, Page: 2, PageSize: 20}}
	projects := stubProjects{list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}}}
	body := renderCommentsPage(t, svc, projects, "?project=p-1&status=pending&page=2", true)
	nav := cmPaginationNav(t, body)

	base := commentsPath + "?" + url.Values{"project": {"p-1"}, "status": {"pending"}}.Encode()
	want := []string{base + "&page=1&limit=20", base + "&page=3&limit=20"}
	sort.Strings(want)
	if got := cmPaginationHrefs(nav); !reflect.DeepEqual(got, want) {
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

func TestCommentsPageSinglePageRendersNoPagination(t *testing.T) {
	for _, total := range []int64{5, 0} {
		svc := &stubCommentSvc{res: &commentdto.AdminListResp{Total: total, Page: 1, PageSize: 20}}
		projects := stubProjects{list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}}}
		body := renderCommentsPage(t, svc, projects, "?project=p-1", true)
		if strings.Contains(body, `<nav class="pagination"`) {
			t.Errorf("total=%d / 每页 20 时不该渲染分页条", total)
		}
	}
}
