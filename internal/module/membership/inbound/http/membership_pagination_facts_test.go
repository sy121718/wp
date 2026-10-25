package membershiphttp

// membership_pagination_facts_test.go — task-116（P3-B3）：会员归属页的分页消费者迁到
// pkg/pagination 事实层后的 handler 级判据。
//
// 判据是「数据与链接字节不变」：迁移前 handler 预制成品（信息行句子 + 链接），
// 迁移后只交事实（pagination.Page），句子与链接由 admin/partials/pagination.html 渲染。
// 期望值用 url.Values **独立**拼出（不拿生产 helper 的结果当期望）。
//
// 本页独有的两处口径：
//   - 基址带工程与筛选（tierId / source / userId），翻页不丢条件；
//   - 装载失败那一档仍然渲染完整页面，但**不注入**分页事实（零值 Page ⇒ 不渲染分页条）。

import (
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

// mbPagSvc 归属页只用四个服务方法（其余方法打到嵌入的 nil 接口上 panic —— 那正是
// 「不该被调用」这条断言想要的表现）。
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

// mbPagProjects 工程契约桩：只实现 List（归属页不查工程详情）。
type mbPagProjects struct {
	projectcontract.ProjectService
	items []projectcontract.ProjectResp
}

func (s mbPagProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return s.items, nil
}

// mbPagRender 用真实渲染器跑一次归属页（handler → 模板），返回响应体。
func mbPagRender(t *testing.T, svc *mbPagSvc, query string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := NewMembershipPageHandle(svc, mbPagProjects{items: []projectcontract.ProjectResp{
		{ID: "p-1", Name: "演示站"},
	}})
	engine := gin.New()
	// 模板根相对包目录：internal/module/membership/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership/assignments", adminui.PageSlot(MembershipAssignmentsPageTemplate), func(c *gin.Context) {
		// 不注入权限上下文：本测试只钉分页事实与链接字节，按钮的可见性由模块既有渲染测试覆盖。
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

// mbPagNav 取页面里的分页条整块。
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

// mbPagHrefs 分页条里全部 href（HTML 还原、去重、排序）。
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

// TestMembershipAssignmentsPaginationIsFactsDriven 链接字节与基址筛选逐条一致。
//
// 两种筛选形态都测：值为 0 / 空的筛选**不进基址**（itoa64/itoaU64 把 0 变空串，
// FilterBaseURL 再丢空值），非空筛选逐条进基址 —— 翻页不丢条件。
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

// TestMembershipAssignmentsSinglePageRendersNoPagination 单页不渲染分页条。
func TestMembershipAssignmentsSinglePageRendersNoPagination(t *testing.T) {
	svc := &mbPagSvc{total: 5}
	body := mbPagRender(t, svc, "?project=p-1&page=1&limit=20")
	if strings.Contains(body, `<nav class="pagination"`) {
		t.Errorf("总计 5 / 每页 20 时不该渲染分页条")
	}
}

// TestMembershipAssignmentsLoadFailureInjectsNoPagination 装载失败档：页面完整，但没有分页条。
func TestMembershipAssignmentsLoadFailureInjectsNoPagination(t *testing.T) {
	svc := &mbPagSvc{total: 45}
	gin.SetMode(gin.TestMode)
	handle := NewMembershipPageHandle(svc, mbPagProjects{})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership/assignments", adminui.PageSlot(MembershipAssignmentsPageTemplate), handle.MembershipAssignmentsPage)
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
