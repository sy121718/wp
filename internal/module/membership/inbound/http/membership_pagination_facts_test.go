package membershiphttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"go_wp/internal/adminui"
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

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

type mbPagSvc struct {
	membershipcontract.MembershipService
	tiers  []*membershipdto.TierResp
	rows   []*membershipdto.AssignmentResp
	total  int64
	counts int
}

func (s *mbPagSvc) ListTiers(context.Context, *membershipdto.ListTiersReq) ([]*membershipdto.TierResp, error) {
	return s.tiers, nil
}

func (s *mbPagSvc) CountAssignments(context.Context, *membershipdto.CountAssignmentsReq) (int64, error) {
	s.counts++
	return s.total, nil
}

func (s *mbPagSvc) ListAssignments(context.Context, *membershipdto.ListAssignmentsReq) ([]*membershipdto.AssignmentResp, error) {
	return s.rows, nil
}

type mbPagProjects struct {
	projectcontract.ProjectService
	items []projectcontract.ProjectResp
}

func (s mbPagProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return s.items, nil
}

func mbPagRender(t *testing.T, svc *mbPagSvc, query string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := NewMembershipPageHandle(svc, mbPagProjects{items: []projectcontract.ProjectResp{
		{ID: "p-1", Name: "演示站"},
	}})
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership/assignments", pageview.Slot(MembershipAssignmentsPageTemplate, adminui.RenderPage), func(c *gin.Context) {

		handle.MembershipAssignmentsPage(c)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/membership/assignments"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d，响应：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

func mbPagNav(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<nav class="pagination"`)
	if i < 0 {
		t.Fatalf("页面里没有分页条（总计 45 / 每页 20 ⇒ 应有 3 页）")
	}
	j := strings.Index(body[i:], "</nav>")
	if j < 0 {
		t.Fatalf("分页条没有闭合")
	}
	return body[i : i+j+len("</nav>")]
}

func mbPagHrefs(nav string) []string {
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

func TestMembershipAssignmentsPaginationIsFactsDriven(t *testing.T) {
	cases := []struct {
		name  string
		query string
		base  string
	}{
		{
			name:  "无有效筛选",
			query: "?project=p-1&tierId=0&source=&userId=0&page=2&limit=20",
			base:  membershipAssignmentsPath + "?" + url.Values{"project": {"p-1"}}.Encode(),
		},
		{
			name:  "带等级来源与访客筛选",
			query: "?project=p-1&tierId=7&source=manual&userId=1024&page=2&limit=20",
			base: membershipAssignmentsPath + "?" + url.Values{
				"project": {"p-1"}, "tierId": {"7"}, "source": {"manual"}, "userId": {"1024"},
			}.Encode(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mbPagSvc{total: 45}
			body := mbPagRender(t, svc, tc.query)
			nav := mbPagNav(t, body)

			want := []string{tc.base + "&page=1&limit=20", tc.base + "&page=3&limit=20"}
			sort.Strings(want)
			if got := mbPagHrefs(nav); !reflect.DeepEqual(got, want) {
				t.Errorf("分页链接字节不符\n实际: %q\n期望: %q", got, want)
			}
			for _, want := range []string{
				`<div class="pagination-info">共 45 条，第 21-40 条</div>`,
				`<span class="pagination-btn is-active">2</span>`,
				`<a class="pagination-btn" href="` + html.EscapeString(tc.base+"&page=1&limit=20") + `">上一页</a>`,
				`<a class="pagination-btn" href="` + html.EscapeString(tc.base+"&page=3&limit=20") + `">下一页</a>`,
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
		})
	}
}

func TestMembershipAssignmentsSinglePageRendersNoPagination(t *testing.T) {
	svc := &mbPagSvc{total: 5}
	body := mbPagRender(t, svc, "?project=p-1&page=1&limit=20")
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("总计 5 / 每页 20 时不该渲染分页条")
	}
}

func TestMembershipAssignmentsLoadFailureInjectsNoPagination(t *testing.T) {
	svc := &mbPagSvc{total: 45}
	gin.SetMode(gin.TestMode)
	handle := NewMembershipPageHandle(svc, mbPagProjects{})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership/assignments", pageview.Slot(MembershipAssignmentsPageTemplate, adminui.RenderPage), handle.MembershipAssignmentsPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/membership/assignments", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染完整页面（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败后页面必须渲染完整（缺 </html>）")
	}
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("工程列表没读到时不该渲染分页条")
	}
	if svc.counts != 0 {
		t.Errorf("装载失败时不应调用归属计数，实际 %d 次", svc.counts)
	}
}
