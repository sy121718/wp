package adminhttp

// admin_pagination_facts_test.go — admin 模块 7 个整页分页站点的数据形态与链接证据。
//
// handler 只注入事实：
//   data["Pagination"] = pagination.Page{Total, Page, Limit, Base}
// 页数、区间句、页码窗口与链接由 admin/partials/pagination.html 用模板层的 pager 算。
// 本文件钉两件事：
//
//  1. 数据形态：只注入事实键（旧成品键 PaginationInfo / PaginationLinks 已无生产者）；
//     PageParam 与「链接带 limit」由 pager 的默认值给出，Base 逐字等于站点实参。
//  2. 链接字节：期望值用 url.Values 独立拼出（不复用 handler 的拼法），任何
//     「Base 丢筛选 / 参数名改名 / size 写错」都会在这里红。
//
// i18n 站点（I18nEntriesPage）的分页走 pkg/i18n 真实读库，见同目录
// admin_pagination_i18n_facts_test.go（外部测试包 + 真 PG）。

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	"go_wp/internal/adminui"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/pagination"
)

// --- 服务桩：只实现站点用到的那一个方法（嵌入接口，其余为 nil，调用即 panic） ---

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

// RuleSchemaList 只被页面用来供「数据域」下拉；这里给空表（页面仍可渲染）。
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

// PermOptions 只被菜单页的新建抽屉用来供权限点候选；空表即「没有可勾的权限点」，页面照常渲染。
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

// --- 录制渲染器：不出 HTML，只把 (模板名, data) 记下来 ---
//
// 为什么需要它：本卡要求「旧实参 → 旧成品键」与「新事实」两份数据渲染结果逐字节相同，
// 而新旧两条路都得从 handler 真实产出的那份 data 出发（自己手搓 data 会测成「测自己」）。
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

// --- 站点表（7 站点的 6 个内存可测项；i18n 站点见另一文件） ---

type adminPaginationSite struct {
	name     string
	path     string // 请求路径：含筛选，不含 page/limit
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

// baseWant 独立拼出期望 Base（与 handler 的 reqparams.FilterBaseURL 同口径，但不复用它的实现）。
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

// linkWant 独立拼出期望链接字节（旧 link() 的语义：base 后用 ?/& 续接 page 与 limit）。
func (s adminPaginationSite) linkWant(page int) string {
	base := s.baseWant()
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "page=" + strconv.Itoa(page) + "&limit=" + strconv.Itoa(s.limit)
}

// newAdminPaginationHandle 用桩装配 6 领域处理器。
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

// captureSiteData 跑一次 handler，返回 (模板名, 真实 data map)，不渲染 HTML。
func captureSiteData(t *testing.T, name string, total int64, target string) (string, gin.H) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := &paginationRecorder{}
	engine := gin.New()
	engine.HTMLRender = rec
	route := strings.SplitN(target, "?", 2)[0]
	// 页面出口改 PageSlot 之后，已迁移的站点要按真实装配方式挂模板名；
	// 未迁移的站点仍由 handler 自己渲染（adminPageTemplateFor 返回空串表示"这批还没迁"）。
	h := siteHandler(newAdminPaginationHandle(total), name)
	if tpl := adminPageTemplateFor(name); tpl != "" {
		engine.GET(route, adminui.PageSlot(tpl), h)
	} else {
		engine.GET(route, h)
	}
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	// 渲染出口交给模板的是 PrepareView 摊平后的 map（键名 = 视图字段名）；
	// 视图换强类型之后不再保证 rec.data 本身是 gin.H，所以统一走 ViewMap 摊平。
	data := adminui.ViewMap(rec.data)
	return rec.name, data
}

// renderAdminPage 用真实 Jet 渲染（磁盘模板），用于逐字节比对。
func renderAdminPage(t *testing.T, name string, data gin.H) string {
	t.Helper()
	renderer := templates.NewJetHTMLRender("../../../../templates", true)
	rec := httptest.NewRecorder()
	inst := renderer.Instance(name, data)
	inst.WriteContentType(rec)
	if err := inst.Render(rec); err != nil {
		t.Fatalf("渲染 %s 失败: %v", name, err)
	}
	return rec.Body.String()
}

// paginationFirstDiff 报告首个差异位置与上下文（对齐 templates 包里 p2FirstDiff 的口径）。
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

// TestAdminPaginationSitesInjectFactsOnly 站点只注入事实键，旧成品键不再出现。
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

// TestAdminPaginationSitesLinkBytes 每站点至少一条链接字节断言（期望值独立拼出）。
func TestAdminPaginationSitesLinkBytes(t *testing.T) {
	for _, s := range adminPaginationSites(45) {
		t.Run(s.name, func(t *testing.T) {
			// 第 1 页：上一页禁用、下一页指向第 2 页、当前页是 is-active 的 1。
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

			// 第 2 页：上一页/下一页都指向正确页码，当前页是 2。
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

// TestAdminPaginationSitesNoNavWhenSinglePageOrEmpty 单页与空集不渲染分页条。
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

// TestAdminI18nEntryFilterPageArithmetic 词条页站点自己的页码/每页算术（纯函数，无库）。
//
// 第 7 站点（I18nEntriesPage）与其余 6 个站点不同：它的页码不是直接读 ?page=，而是
// 由 filter.Offset / adminI18nEntryPageSize 反推、且每页条数是常量（不看 ?limit=）。
// 这里钉住这组算术 —— 它是交给 Facts 的 total/page/size 三者里 page/size 的来源。
// 该站点的真库渲染证据见同目录 admin_pagination_i18n_facts_test.go。
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
			// 事实层的 size 必须是同一常量：链接里的 limit=50 就是从这里来的。
			pg := pagination.Page{Total: 120, Page: page, Limit: adminI18nEntryPageSize}
			if pg.Limit != tc.wantLimit {
				t.Fatalf("事实层 limit 应为 %d，实际 %d", tc.wantLimit, pg.Limit)
			}
		})
	}
}

// withoutFilters 同一个站点、去掉筛选的请求（覆盖「base 无查询串」这一档）。
func (s adminPaginationSite) withoutFilters() adminPaginationSite {
	c := s
	c.path = s.basePath
	c.filters = nil
	return c
}

// adminPageTemplateFor 页面名 → 模板名（只覆盖**已迁移**到 PageSlot 的站点）。
//
// 返回空串表示这个站点还没迁：测试装置不替 handler 决定出口形态。
// 每迁完一个页面就往这里加一行 —— 它同时是「这个模块迁到哪了」的清单。
func adminPageTemplateFor(name string) string {
	if name == "administrators" {
		return AdministratorsPageTemplate
	}
	return ""
}
