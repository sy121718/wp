package masterdatahttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

type mdPagChanges struct {
	masterdatacontract.MasterDataService
	total int64
}

func (s *mdPagChanges) ListChanges(context.Context, *masterdatadto.ListChangeReq) ([]*masterdatadto.ChangeResp, error) {
	return nil, nil
}

func (s *mdPagChanges) CountChanges(context.Context, *masterdatadto.ListChangeReq) (int64, error) {
	return s.total, nil
}

func (s *mdPagChanges) ListEntities(context.Context, *masterdatadto.ListEntityReq) ([]*masterdatadto.EntityHistoryResp, error) {
	return nil, nil
}

func (s *mdPagChanges) CountEntities(context.Context, *masterdatadto.ListEntityReq) (int64, error) {
	return 0, nil
}

type mdPagProjects struct {
	projectcontract.ProjectService
}

func (mdPagProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}}, nil
}

func mdPagRender(t *testing.T, changes *mdPagChanges, query string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := NewMasterDataChangePageHandle(changes, mdPagProjects{})
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)

	engine.GET("/admin/masterdata/changes",
		pageview.Slot(MasterDataChangesTemplate, pageview.RenderPage),
		handle.MasterDataChangesPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/masterdata/changes"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d，响应：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

func mdPagNav(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<nav class="pagination"`)
	if i < 0 {
		t.Fatalf("页面里没有分页条（变更记录 45 / 每页 20 ⇒ 应有 3 页）")
	}
	j := strings.Index(body[i:], "</nav>")
	if j < 0 {
		t.Fatalf("分页条没有闭合")
	}
	return body[i : i+j+len("</nav>")]
}

func mdPagHrefs(nav string) []string {
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

func TestMasterDataChangesPaginationIsFactsDriven(t *testing.T) {
	body := mdPagRender(t, &mdPagChanges{total: 45}, "?project=p-1&keyword=价格&page=2&limit=20")
	nav := mdPagNav(t, body)

	base := "/admin/masterdata/changes?" + url.Values{
		"project": {"p-1"}, "keyword": {"价格"},
	}.Encode()
	want := []string{base + "&page=1&limit=20", base + "&page=3&limit=20"}
	sort.Strings(want)
	if got := mdPagHrefs(nav); !reflect.DeepEqual(got, want) {
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

func TestMasterDataChangesSinglePageRendersNoPagination(t *testing.T) {
	body := mdPagRender(t, &mdPagChanges{total: 5}, "?project=p-1&page=1&limit=20")
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("变更记录 5 条 / 每页 20 时不该渲染分页条")
	}
}
