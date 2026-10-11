package commenthttp

// comment_pagination_facts_test.go — task-116（P3-B3）：评论审核页的分页消费者迁到
// pkg/pagination 事实层后的 handler 级判据。
//
// 判据是「渲染字节不变」：迁移前 handler 用 BuildPagination 预制成品（信息行句子 + 链接），
// 迁移后只交事实（pagination.Page），句子与链接由 admin/partials/pagination.html 渲染。
// 期望值用 url.Values **独立**拼出（不拿生产 helper 的结果当期望），基址必须带全部筛选条件
// —— 翻页不丢条件（这是本页筛选栏最容易漏的一处）。

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

// cmPaginationNav 取页面里的分页条整块。
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

// cmPaginationHrefs 分页条里全部 href（HTML 还原、去重、排序）。
//
// 去重的原因：「上一页」与页码 1 指向同一 URL；重数由整元素断言单独钉住。
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

// TestCommentsPagePaginationIsFactsDriven 链接字节与基址筛选逐条一致。
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

// TestCommentsPageSinglePageRendersNoPagination 单页与空集都不渲染分页条。
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
