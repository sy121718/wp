package adminhttp

import (
	"context"
	pageview "go_wp/pkg/view"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/pagination"
	text "go_wp/pkg/view/jet"
)

type paginationAdminsStub struct {
	admincontract.AdminService
	total int64
}

func (s *paginationAdminsStub) AdminList(_ context.Context, _ *admindto.AdminListReq) (*admindto.AdminListResp, error) {
	return &admindto.AdminListResp{Total: s.total}, nil
}

type paginationRulesStub struct {
	admincontract.RuleService
	total int64
}

func (s *paginationRulesStub) RuleList(_ context.Context, _ *admindto.RuleListReq) (*admindto.RuleListResp, error) {
	return &admindto.RuleListResp{Total: s.total}, nil
}

func (s *paginationRulesStub) RuleSchemaList(_ context.Context) ([]admindto.RuleDomainItem, error) {
	return nil, nil
}

type paginationDeptsStub struct {
	admincontract.DeptService
	total int64
}

func (s *paginationDeptsStub) DeptPage(_ context.Context, _, _ int, _ string) (*admindto.DeptPageResp, error) {
	return &admindto.DeptPageResp{Total: s.total}, nil
}

type paginationMenusStub struct {
	admincontract.MenuService
	total int64
}

func (s *paginationMenusStub) MenuPage(_ context.Context, _, _ int, _ string) (*admindto.MenuPageResp, error) {
	return &admindto.MenuPageResp{Total: s.total}, nil
}

type paginationPermsStub struct {
	admincontract.PermService
	total int64
}

func (s *paginationPermsStub) PermList(_ context.Context, _ *admindto.PermListReq) (*admindto.PermListResp, error) {
	return &admindto.PermListResp{Total: s.total}, nil
}

func (s *paginationPermsStub) PermOptions(_ context.Context, _ *admindto.PermOptionsReq) (*admindto.PermOptionsResp, error) {
	return &admindto.PermOptionsResp{}, nil
}

type paginationRolesStub struct {
	admincontract.RoleService
	total int64
}

func (s *paginationRolesStub) RoleList(_ context.Context, _ *admindto.RoleListReq) (*admindto.RoleListResp, error) {
	return &admindto.RoleListResp{Total: s.total}, nil
}

type paginationRecorder struct {
	name string
	data any
}

func (r *paginationRecorder) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data
	return r
}

func (r *paginationRecorder) Render(http.ResponseWriter) error     { return nil }
func (r *paginationRecorder) WriteContentType(http.ResponseWriter) {}

type adminPaginationSite struct {
	name     string
	path     string
	template string
	total    int64
	limit    int
	basePath string
	filters  map[string]string
}

func adminPaginationSites(total int64) []adminPaginationSite {
	return []adminPaginationSite{
		{
			name: "administrators", path: "/admin/administrators?name=alice&email=a%40b.com",
			template: "admin/system/administrators", total: total, limit: 20,
			basePath: "/admin/administrators",
			filters:  map[string]string{"name": "alice", "email": "a@b.com"},
		},
		{
			name: "datarules", path: "/admin/datarules?domain=ADMIN",
			template: "admin/system/datarules", total: total, limit: 20,
			basePath: "/admin/datarules",
			filters:  map[string]string{"domain": "ADMIN"},
		},
		{
			name: "departments", path: "/admin/departments?keyword=dept",
			template: "admin/system/departments", total: total, limit: 20,
			basePath: "/admin/departments",
			filters:  map[string]string{"keyword": "dept"},
		},
		{
			name: "menus", path: "/admin/menus?keyword=sys",
			template: "admin/system/menus", total: total, limit: 20,
			basePath: "/admin/menus",
			filters:  map[string]string{"keyword": "sys"},
		},
		{
			name: "permissions", path: "/admin/permissions?code=user%3Alist&module=user",
			template: "admin/system/permissions", total: total, limit: 20,
			basePath: "/admin/permissions",
			filters:  map[string]string{"code": "user:list", "module": "user"},
		},
		{
			name: "roles", path: "/admin/roles?keyword=admin",
			template: "admin/system/roles", total: total, limit: 20,
			basePath: "/admin/roles",
			filters:  map[string]string{"keyword": "admin"},
		},
	}
}

func (s adminPaginationSite) baseWant() string {
	q := url.Values{}
	for k, v := range s.filters {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return s.basePath
	}
	return s.basePath + "?" + q.Encode()
}

func (s adminPaginationSite) linkWant(page int) string {
	base := s.baseWant()
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "page=" + strconv.Itoa(page) + "&limit=" + strconv.Itoa(s.limit)
}

func newAdminPaginationHandle(total int64) *AdminPagesHandle {
	return NewAdminPagesHandle(
		&paginationAdminsStub{total: total},
		&paginationRolesStub{total: total},
		&paginationPermsStub{total: total},
		&paginationMenusStub{total: total},
		&paginationDeptsStub{total: total},
		&paginationRulesStub{total: total},
	)
}

func siteHandler(h *AdminPagesHandle, name string) gin.HandlerFunc {
	switch name {
	case "administrators":
		return h.AdministratorsPage
	case "datarules":
		return h.DatarulesPage
	case "departments":
		return h.DepartmentsPage
	case "menus":
		return h.MenusPage
	case "permissions":
		return h.PermissionsPage
	case "roles":
		return h.RolesPage
	}
	panic("未知站点 " + name)
}

func captureSiteData(t *testing.T, name string, total int64, target string) (string, gin.H) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := &paginationRecorder{}
	engine := gin.New()
	engine.HTMLRender = rec
	route := strings.SplitN(target, "?", 2)[0]

	h := siteHandler(newAdminPaginationHandle(total), name)
	if tpl := adminPageTemplateFor(name); tpl != "" {
		engine.GET(route, pageview.Slot(tpl, pageview.RenderPage), h)
	} else {
		engine.GET(route, h)
	}
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))

	data := pageview.ViewMap(rec.data)
	return rec.name, data
}

// renderAdminPage 复刻生产渲染接线：取词函数经 ScopeWriter 注入，
// 缺它时模板里的 t / tr 全部不可用（生产由 pkg/view/jet.AttachScope 挂上）。
func renderAdminPage(t *testing.T, name string, data gin.H) string {
	t.Helper()
	renderer := templates.NewJetHTMLRender("../../../../templates", true)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	text.AttachScope(c)
	inst := renderer.Instance(name, data)
	inst.WriteContentType(c.Writer)
	if err := inst.Render(c.Writer); err != nil {
		t.Fatalf("渲染 %s 失败: %v", name, err)
	}
	return rec.Body.String()
}

func paginationFirstDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	i := 0
	for i < n && want[i] == got[i] {
		i++
	}
	lo := i - 60
	if lo < 0 {
		lo = 0
	}
	hiW, hiG := i+60, i+60
	if hiW > len(want) {
		hiW = len(want)
	}
	if hiG > len(got) {
		hiG = len(got)
	}
	return "首个差异偏移 " + strconv.Itoa(i) + "\n旧: " + want[lo:hiW] + "\n新: " + got[lo:hiG]
}

const navOpen = "class=\"pagination\""

func TestAdminPaginationSitesInjectFactsOnly(t *testing.T) {
	for _, s := range adminPaginationSites(45) {
		t.Run(s.name, func(t *testing.T) {
			_, data := captureSiteData(t, s.name, s.total, s.path)
			raw, ok := data["Pagination"]
			if !ok {
				t.Fatalf("%s：应注入 Pagination 事实键", s.name)
			}
			pg, ok := raw.(pagination.Page)
			if !ok {
				t.Fatalf("%s：Pagination 应是 pagination.Page（值类型），实际 %T", s.name, raw)
			}
			if pg.Base != s.baseWant() {
				t.Fatalf("%s：Base 应为 %q，实际 %q", s.name, s.baseWant(), pg.Base)
			}
			if pager := templates.PagerOf(pg); pager.PageParam != "page" || !pager.KeepLimit {
				t.Fatalf("%s：默认参数应为 page 且带 limit，实际 %q/keepLimit=%v", s.name, pager.PageParam, pager.KeepLimit)
			}
			if pg.Total != s.total || pg.Limit != s.limit || pg.Page != 1 {
				t.Fatalf("%s：事实应为 total=%d limit=%d page=1，实际 %d/%d/%d",
					s.name, s.total, s.limit, pg.Total, pg.Limit, pg.Page)
			}
			for _, legacy := range []string{"PaginationInfo", "PaginationLinks"} {
				if _, has := data[legacy]; has {
					t.Fatalf("%s：旧成品键 %s 不应再注入（迁移后本模块已无消费者）", s.name, legacy)
				}
			}
		})
	}
}

func TestAdminPaginationSitesLinkBytes(t *testing.T) {
	for _, s := range adminPaginationSites(45) {
		t.Run(s.name, func(t *testing.T) {

			tmpl, data := captureSiteData(t, s.name, s.total, s.path)
			got := renderAdminPage(t, tmpl, data)
			nextWant := "href=\"" + html.EscapeString(s.linkWant(2)) + "\""
			if !strings.Contains(got, nextWant) {
				t.Fatalf("%s：第 1 页应出现下一页链接 %s", s.name, nextWant)
			}
			if !strings.Contains(got, "<span class=\"pagination-btn is-active\">1</span>") {
				t.Fatalf("%s：第 1 页应是 is-active 的 1\n%s", s.name, got)
			}
			if !strings.Contains(got, "<span class=\"pagination-btn is-disabled\">上一页</span>") {
				t.Fatalf("%s：第 1 页上一页应禁用", s.name)
			}

			tmpl2, data2 := captureSiteData(t, s.name, s.total, s.path+"&page=2")
			got2 := renderAdminPage(t, tmpl2, data2)
			for _, want := range []int{1, 3} {
				href := "href=\"" + html.EscapeString(s.linkWant(want)) + "\""
				if !strings.Contains(got2, href) {
					t.Fatalf("%s：第 2 页应出现指向第 %d 页的链接 %s", s.name, want, href)
				}
			}
			if !strings.Contains(got2, "<span class=\"pagination-btn is-active\">2</span>") {
				t.Fatalf("%s：第 2 页应是 is-active 的 2", s.name)
			}
		})
	}
}

func TestAdminPaginationSitesNoNavWhenSinglePageOrEmpty(t *testing.T) {
	for _, total := range []int64{0, 20} {
		for _, s := range adminPaginationSites(total) {
			s.total = total
			t.Run(s.name+"/total="+strconv.FormatInt(total, 10), func(t *testing.T) {
				tmpl, data := captureSiteData(t, s.name, total, s.path)
				got := renderAdminPage(t, tmpl, data)
				if strings.Contains(got, navOpen) {
					t.Fatalf("%s：total=%d 时不应渲染分页条\n%s", s.name, total, got)
				}
			})
		}
	}
}

func TestAdminI18nEntryFilterPageArithmetic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		query      string
		wantOffset int
		wantLimit  int
		wantPage   int
	}{
		{"无参数", "", 0, adminI18nEntryPageSize, 1},
		{"第1页", "?page=1", 0, adminI18nEntryPageSize, 1},
		{"第2页", "?page=2", adminI18nEntryPageSize, adminI18nEntryPageSize, 2},
		{"第7页", "?page=7", 6 * adminI18nEntryPageSize, adminI18nEntryPageSize, 7},
		{"页码 0 归 1", "?page=0", 0, adminI18nEntryPageSize, 1},
		{"负页码归 1", "?page=-3", 0, adminI18nEntryPageSize, 1},
		{"非数字归 1", "?page=abc", 0, adminI18nEntryPageSize, 1},
		{"忽略 limit", "?limit=5&page=2", adminI18nEntryPageSize, adminI18nEntryPageSize, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/admin/i18n"+tc.query, nil)
			f := adminI18nEntryFilterOf(c)
			if f.Offset != tc.wantOffset || f.Limit != tc.wantLimit {
				t.Fatalf("filter 应为 offset=%d limit=%d，实际 offset=%d limit=%d",
					tc.wantOffset, tc.wantLimit, f.Offset, f.Limit)
			}
			page := f.Offset/adminI18nEntryPageSize + 1
			if page != tc.wantPage {
				t.Fatalf("反推页码应为 %d，实际 %d", tc.wantPage, page)
			}

			pg := pagination.Page{Total: 120, Page: page, Limit: adminI18nEntryPageSize}
			if pg.Limit != tc.wantLimit {
				t.Fatalf("事实层 limit 应为 %d，实际 %d", tc.wantLimit, pg.Limit)
			}
		})
	}
}

func (s adminPaginationSite) withoutFilters() adminPaginationSite {
	c := s
	c.path = s.basePath
	c.filters = nil
	return c
}

func adminPageTemplateFor(name string) string {
	switch name {
	case "administrators":
		return AdministratorsPageTemplate
	case "datarules":
		return DatarulesPageTemplate
	case "departments":
		return DepartmentsPageTemplate
	case "menus":
		return MenusPageTemplate
	case "roles":
		return RolesPageTemplate
	case "permissions":
		return PermissionsPageTemplate
	}
	return ""
}
